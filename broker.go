package main

type Broker interface {
	Publish(msg []byte)
	Subscribe() (chan []byte, error)
	Unsubscribe(ch []byte)
}

type Registrar interface {
	Register(id UUID) error
	IsRegistered(id UUID) bool
}
