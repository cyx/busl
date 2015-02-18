package broker

import (
	"io"

	"github.com/heroku/busl/util"
)

type Reader struct {
	broker *RedisBroker
	ch     chan []byte
}

func NewReader(uuid util.UUID) (*Reader, error) {
	broker := NewRedisBroker(uuid)
	ch, err := broker.Subscribe()

	if err != nil {
		return nil, err
	}

	return &Reader{broker: broker, ch: ch}, nil
}

func (r *Reader) Read(p []byte) (n int, err error) {
	msg, ok := <-r.ch

	if !ok {
		err = io.EOF
	}

	if n = len(msg); n > 0 {
		copy(p, msg)
	}

	return n, err
}

func (r *Reader) Close() {
	r.broker.Unsubscribe(r.ch)
}
