package util

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"
)

type UUID string

func NewUUID() (UUID, error) {
	uuid := make([]byte, 16)
	n, err := rand.Read(uuid)
	if n != len(uuid) || err != nil {
		return "", err
	}

	uuid[8] = 0x80 // variant bits see page 5
	uuid[4] = 0x40 // version 4 Pseudo Random, see page 7

	return UUID(hex.EncodeToString(uuid)), nil
}

type NullByte []byte

func GetNullByte() []byte {
	return new(NullByte).Get()
}

func (nb NullByte) Get() []byte {
	nb = []byte{0}
	return nb
}

type StringSliceUtil []string

func (s StringSliceUtil) Contains(check string) bool {
	for _, c := range s {
		if c == check {
			return true
		}
	}
	return false
}

func Count(metric string) { CountMany(metric, 1) }

func CountMany(metric string, count int64) { CountWithData(metric, count, "") }

func CountWithData(metric string, count int64, extraData string, v ...interface{}) {
	if extraData == "" {
		log.Printf("count#%s=%d", metric, count)
	} else {
		log.Printf("count#%s=%d %s", metric, count, fmt.Sprintf(extraData, v))
	}
}

func TimeoutFunc(d time.Duration, ƒ func()) (ch chan bool) {
	ch = make(chan bool)
	time.AfterFunc(d, func() {
		ch <- false
	})
	go func() {
		ƒ()
		ch <- true
	}()
	return ch
}

func AwaitSignals(signals ...os.Signal) <-chan struct{} {
	s := make(chan os.Signal, 1)
	signal.Notify(s, signals...)
	log.Printf("signals.await signals=%v\n", signals)

	received := make(chan struct{})
	go func() {
		log.Printf("signals.received signal=%v\n", <-s)
		close(received)
	}()

	return received
}

func RequestId(r *http.Request) (id string) {
	if id = r.Header.Get("Request-Id"); id == "" {
		id = r.Header.Get("X-Request-Id")
	}

	if id == "" {
		// Given that getting errors from /dev/urandom
		// should be a rare enough event, coupled with
		// the fact that having an empty request_id
		// shouldn't interrupt other parts of busl
		// then better to just continue from here
		// with an empty string.
		uuid, _ := NewUUID()
		id = string(uuid)
	}

	return id
}
