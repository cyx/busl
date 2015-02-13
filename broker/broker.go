package broker

import (
	"github.com/heroku/busl/util"
)

type Broker interface {
	Publish(msg []byte)
	Subscribe(offset int64) (chan []byte, error)
	Unsubscribe(ch chan []byte)
}

type Registrar interface {
	Register(id util.UUID) error
	IsRegistered(id util.UUID) bool
}
