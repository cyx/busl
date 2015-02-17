package broker

import (
	"errors"
	"flag"
	"io"
	"log"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/garyburd/redigo/redis"
	"github.com/heroku/busl/util"
)

const (
	msgBuf = 10
)

var (
	redisUrl           = flag.String("redisUrl", os.Getenv("REDIS_URL"), "URL of the redis server")
	redisServer        *url.URL
	redisPool          *redis.Pool
	redisKeyExpire     = 60 // redis uses seconds for EXPIRE
	redisChannelExpire = redisKeyExpire * 5
	luaFetchSha1       = []byte{}
	luaPublishSha1     = []byte{}
)

func init() {
	flag.Parse()
	redisServer, _ = url.Parse(*redisUrl)
	redisPool = newPool(redisServer)

	conn := redisPool.Get()
	defer conn.Close()

	luaFetchSha1, _ = redis.Bytes(conn.Do("SCRIPT", "LOAD", luaFetch))
	luaPublishSha1, _ = redis.Bytes(conn.Do("SCRIPT", "LOAD", luaPublish))
}

func newPool(server *url.URL) *redis.Pool {
	cleanServerURL := *server
	cleanServerURL.User = nil
	log.Printf("connecting to redis: %s", cleanServerURL)
	return &redis.Pool{
		MaxIdle:     3,
		IdleTimeout: 4 * time.Minute,
		Dial: func() (c redis.Conn, err error) {
			c, err = redis.Dial("tcp", server.Host)
			if err != nil {
				return
			}

			if server.User == nil {
				return
			}

			pw, pwset := server.User.Password()
			if !pwset {
				return
			}

			if _, err = c.Do("AUTH", pw); err != nil {
				c.Close()
				return
			}
			return
		},
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			_, err := c.Do("PING")
			return err
		},
	}
}

type channel string

func (c channel) id() string {
	return string(c)
}

func (c channel) wildcardId() string {
	return string(c) + "*"
}

func (c channel) uuid() util.UUID {
	return util.UUID(c)
}

func (c channel) doneId() string {
	return string(c) + "done"
}

func (c channel) killId() string {
	return string(c) + "kill"
}

type RedisBroker struct {
	channel     channel
	subscribers map[chan []byte]bool
	psc         redis.PubSubConn
	mutex       *sync.Mutex
}

func NewRedisBroker(uuid util.UUID) *RedisBroker {
	broker := &RedisBroker{
		channel(uuid),
		make(map[chan []byte]bool),
		redis.PubSubConn{},
		&sync.Mutex{},
	}

	return broker
}

func (b *RedisBroker) Subscribe(offset int64) (ch chan []byte, err error) {
	if !NewRedisRegistrar().IsRegistered(b.channel.uuid()) {
		return nil, errors.New("Channel is not registered.")
	}

	b.mutex.Lock()
	defer b.mutex.Unlock()
	ch = make(chan []byte, msgBuf)
	b.subscribers[ch] = true
	go b.redisSubscribe(ch, offset)
	return
}

func (b *RedisBroker) redisSubscribe(ch chan []byte, offset int64) {
	conn := redisPool.Get()
	defer conn.Close()

	if n, err := b.replay(ch, offset); err != nil {
		b.Unsubscribe(ch)
		return
	} else {
		offset += int64(n)
	}

	b.psc = redis.PubSubConn{conn}
	b.psc.PSubscribe(b.channel.wildcardId())

	for {
		switch msg := b.psc.Receive().(type) {
		case redis.PMessage:
			switch msg.Channel {
			case b.channel.killId():
				util.Count("RedisBroker.redisSubscribe.Channel.kill")
				b.psc.PUnsubscribe(b.channel.wildcardId())
			case b.channel.id():
				if b.subscribers[ch] {
					data, _ := b.getRange(offset, msg.Data)
					offset += int64(len(data))
					ch <- data
				} else {
					return
				}
			}
		case redis.Subscription:
			if msg.Kind == "punsubscribe" || msg.Kind == "unsubscribe" {
				subscSlice, _ := b.getRange(offset, []byte("-1"))
				ch <- subscSlice

				util.Count("RedisBroker.redisSubscribe.Channel.unsubscribe")
				b.Unsubscribe(ch)
				return
			}
		case error:
			util.CountWithData("RedisBroker.redisSubscribe.RecieveError", 1, "err=%s", msg)
			return
		}
	}
}

func (b *RedisBroker) Unsubscribe(ch chan []byte) {
	if b.subscribers[ch] {
		close(ch)
	}

	b.mutex.Lock()
	defer b.mutex.Unlock()
	delete(b.subscribers, ch)
}

func (b *RedisBroker) UnsubscribeAll() {
	util.CountMany("RedisBroker.UnsubscribeAll", int64(len(b.subscribers)))

	conn := redisPool.Get()
	defer conn.Close()

	_, err := conn.Do("PUBLISH", b.channel.killId(), []byte{1})
	if err != nil {
		util.CountWithData("RedisBroker.publishOn.error", 1, "error=%s", err)
	}

	conn.Do("SETEX", b.channel.doneId(), redisChannelExpire, []byte{1})
}

func (b *RedisBroker) Write(msg []byte) (n int, err error) {
	err = b.publishOn(msg)

	if err != nil {
		return 0, err
	}
	return len(msg), nil
}

func (b *RedisBroker) publishOn(msg []byte) error {
	conn := redisPool.Get()
	defer conn.Close()

	conn.Send("MULTI")
	conn.Send("APPEND", b.channel.id(), msg)
	conn.Send("EXPIRE", b.channel.id(), redisKeyExpire)
	conn.Send("DEL", b.channel.doneId())

	appendedVals, err := redis.Values(conn.Do("EXEC"))
	if err != nil {
		util.CountWithData("RedisBroker.publishOn.error", 1, "error=%s", err)
		return err
	}
	appendedLen := appendedVals[0].(int64)

	return conn.Send("PUBLISH", b.channel.id(), appendedLen)
}

func (b *RedisBroker) replay(ch chan []byte, offset int64) (n int, err error) {
	if _, channelExists := b.subscribers[ch]; !channelExists {
		return 0, errors.New("Channel already closed.")
	}

	data, err := b.getRange(offset, []byte("-1"))

	if data != nil {
		ch <- data
	}

	if err == io.EOF {
		util.Count("RedisBroker.replay.channelDone")
		return len(data), err
	} else if err != nil {
		util.CountWithData("RedisBroker.publishOn.error", 1, "error=%s", err)
		return 0, err
	}
	return len(data), err
}

func redisEvalSha(sha string, v ...interface{}) {
	conn := redisPool.Get()
	defer conn.Close()
}

func (b *RedisBroker) getRange(start int64, end []byte) ([]byte, error) {
	conn := redisPool.Get()
	defer conn.Close()

	results, err := redis.Values(conn.Do(
		"EVALSHA", luaFetchSha1,
		2, b.channel.id(), b.channel.doneId(),
		start, end))

	if err != nil {
		log.Println(err)
		return []byte{}, err
	}

	data := getRedisByteArray(results[0])
	done := getRedisByteArray(results[1])

	if done != nil && done[0] == 1 {
		err = io.EOF
	}

	return data, err
}

func getRedisByteArray(v interface{}) []byte {
	if v != nil {
		return v.([]byte)
	}
	return nil
}

type RedisRegistrar struct{}

func NewRedisRegistrar() *RedisRegistrar {
	registrar := &RedisRegistrar{}

	return registrar
}

func (rr *RedisRegistrar) Register(channel util.UUID) (err error) {
	conn := redisPool.Get()
	defer conn.Close()

	_, err = conn.Do("SETEX", channel, redisChannelExpire, make([]byte, 0))
	if err != nil {
		util.CountWithData("RedisRegistrar.Register.error", 1, "error=%s", err)
		return
	}
	return
}

func (rr *RedisRegistrar) IsRegistered(channel util.UUID) (registered bool) {
	conn := redisPool.Get()
	defer conn.Close()

	result, err := redis.Int64(conn.Do("EXISTS", channel))
	if err != nil {
		util.CountWithData("RedisRegistrar.IsRegistered.error", 1, "error=%s", err)
		return false
	}

	return result == 1
}

const luaFetch = `
local uuid = KEYS[1]
local done = KEYS[2]
local start = tonumber(ARGV[1])
local finish = tonumber(ARGV[2])

if start == 0 and finish == -1 then
        return redis.call("MGET", uuid, done)
else
	return {redis.call("GETRANGE", uuid, start, finish), redis.call("GET", done)}
end
`

const luaPublish = `
local id   = KEYS[1]
local done = KEYS[2]
local msg  = ARGV[1]
local ttl  = ARGV[2]

redis.call("APPEND", id, msg)
redis.call("EXPIRE", id, ttl)
redis.call("DEL", done)
redis.call("PUBLISH", id, #msg)

return 1
`
