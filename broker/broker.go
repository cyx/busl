package broker

import (
	"io"

	"github.com/heroku/busl/util"
)

type Broker interface {
	io.Writer
	Subscribe(offset int64) (chan []byte, error)
	Unsubscribe(ch chan []byte)
}

type Registrar interface {
	Register(id util.UUID) error
	IsRegistered(id util.UUID) bool
}
