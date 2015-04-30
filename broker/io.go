package broker

import (
	"errors"
	"io"
	"sync"

	"github.com/heroku/busl/Godeps/_workspace/src/github.com/garyburd/redigo/redis"
	"github.com/heroku/busl/util"
)

type writer struct {
	channel channel
}

var ErrNotRegistered = errors.New("Channel is not registered.")

func NewWriter(key string) (io.WriteCloser, error) {
	if !NewRedisRegistrar().IsRegistered(key) {
		return nil, ErrNotRegistered
	}

	return &writer{channel(key)}, nil
}

func (w *writer) Close() error {
	conn := redisPool.Get()
	defer conn.Close()

	conn.Send("MULTI")
	conn.Send("SETEX", w.channel.doneId(), redisChannelExpire, []byte{1})
	conn.Send("PUBLISH", w.channel.killId(), 1)
	_, err := conn.Do("EXEC")
	return err
}

func (w *writer) Write(p []byte) (int, error) {
	conn := redisPool.Get()
	defer conn.Close()

	conn.Send("MULTI")
	conn.Send("APPEND", w.channel.id(), p)
	conn.Send("EXPIRE", w.channel.id(), redisChannelExpire)
	conn.Send("DEL", w.channel.doneId())
	conn.Send("PUBLISH", w.channel.id(), 1)

	_, err := conn.Do("EXEC")
	return len(p), err
}

type reader struct {
	channel  channel
	psc      redis.PubSubConn
	offset   int64
	replayed bool
	closed   bool
	mutex    *sync.Mutex
}

func NewReader(key string) (io.ReadCloser, error) {
	if !NewRedisRegistrar().IsRegistered(key) {
		return nil, ErrNotRegistered
	}

	psc := redis.PubSubConn{redisPool.Get()}
	channel := channel(key)
	psc.PSubscribe(channel.wildcardId())

	rd := &reader{
		channel: channel,
		psc:     psc,
		mutex:   &sync.Mutex{}}

	return rd, nil
}

var errWhence = errors.New("Seek: invalid whence")
var errOffset = errors.New("Seek: invalid offset")

func (r *reader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	default:
		return 0, errWhence
	case 0:
		r.offset = offset
	case 1:
		r.offset += offset
	}
	if offset < 0 {
		return 0, errOffset
	}

	return r.offset, nil
}

func (r *reader) Read(p []byte) (n int, err error) {
	if r.closed { // Don't read from a closed redigo connection
		return 0, io.EOF
	}

	if n, err := r.replay(p); n > 0 || err != nil {
		return n, err
	}

	switch msg := r.psc.Receive().(type) {
	case redis.Message:
	case redis.PMessage:
		return r.read(msg, p)
	case redis.Subscription:
	case error:
		util.CountWithData("RedisBroker.redisSubscribe.RecieveError", 1, "err=%s", msg)
		err = msg
		return
	}
	return
}

func (r *reader) replay(p []byte) (n int, err error) {
	var buf []byte

	if !r.replayed {
		r.replayed = true

		buf, err = r.fetch()
		n = len(buf)

		if n > 0 {
			r.offset += int64(n)
			copy(p, buf)
		}

		if err == io.EOF {
			util.Count("RedisBroker.replay.channelDone")
		}
	}

	return n, err
}

func (r *reader) read(msg redis.PMessage, p []byte) (n int, err error) {
	var buf []byte

	if msg.Channel == r.channel.id() {
		buf, err = r.fetch()

		if n = len(buf); n > 0 {
			copy(p, buf)
			r.offset += int64(n)
		}
	}

	if msg.Channel == r.channel.killId() || err == io.EOF {
		util.Count("RedisBroker.redisSubscribe.Channel.kill")
		r.Close()
		err = io.EOF
	}

	return n, err
}

func (r *reader) fetch() ([]byte, error) {
	conn := redisPool.Get()
	defer conn.Close()

	conn.Send("MULTI")
	conn.Send("GETRANGE", r.channel.id(), r.offset, -1)
	conn.Send("EXISTS", r.channel.doneId())
	conn.Send("EXPIRE", r.channel.id(), redisChannelExpire)

	list, err := redis.Values(conn.Do("EXEC"))
	data, err := redis.Bytes(list[0], err)
	done, err := redis.Bool(list[1], err)

	if done {
		err = io.EOF
	}

	return data, err
}

func (r *reader) Close() error {
	if r.closed {
		return nil
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.closed = true
	r.psc.Unsubscribe()
	return r.psc.Close()
}

func ReaderDone(rd io.Reader) bool {
	r, ok := rd.(*reader)
	if !ok {
		return false
	}

	if r.closed {
		return true
	}

	conn := redisPool.Get()
	defer conn.Close()

	done, _ := redis.Bool(conn.Do("EXISTS", r.channel.doneId()))
	return done
}

func NoContent(rd io.Reader, offset int64) bool {
	if !ReaderDone(rd) {
		return false
	}

	conn := redisPool.Get()
	defer conn.Close()

	strlen, err := redis.Int64(conn.Do("STRLEN", rd.(*reader).channel.id()))
	if err != nil {
		return false
	}

	return offset > (strlen - 1)
}
