package broker

import (
	"fmt"
	"io"
	"os"

	"github.com/heroku/busl/util"
)

func setup() (*RedisBroker, util.UUID) {
	registrar := NewRedisRegistrar()
	uuid, _ := util.NewUUID()
	registrar.Register(uuid)

	return NewRedisBroker(uuid), uuid
}

func ExamplePubSub() {
	b, uuid := setup()

	reader, _ := NewReader(uuid)
	defer reader.Close()

	pub := make(chan bool)
	done := make(chan bool)

	go func() {
		<-pub
		io.Copy(os.Stdout, reader)
		done <- true
	}()

	go func() {
		pub <- true

		b.Write([]byte("busl"))
		b.Write([]byte(" hello"))
		b.Write([]byte(" world"))
		b.UnsubscribeAll()
	}()

	<-done

	//Output:
	// busl hello world
}

func ExampleFullReplay() {
	b, uuid := setup()

	b.Write([]byte("busl"))
	b.Write([]byte(" hello"))
	b.Write([]byte(" world"))

	reader, _ := NewReader(uuid)
	defer reader.Close()

	buf := make([]byte, 16)
	io.ReadAtLeast(reader, buf, 16)

	fmt.Printf("%s", buf)

	//Output:
	// busl hello world
}

func ExampleHalfReplayHalfSubscribed() {
	b, uuid := setup()
	b.Write([]byte("busl"))

	reader, _ := NewReader(uuid)

	pub := make(chan bool)
	done := make(chan bool)

	go func() {
		<-pub
		io.Copy(os.Stdout, reader)
		done <- true
	}()

	go func() {
		pub <- true

		b.Write([]byte(" hello"))
		b.Write([]byte(" world"))
		b.UnsubscribeAll()
	}()

	<-done

	//Output:
	// busl hello world
}
