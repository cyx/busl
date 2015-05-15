package broker

import (
	"testing"

	"github.com/heroku/busl/util"
)

func redisTestSetup() (string, *RedisRegistrar) {
	uuid, _ := util.NewUUID()

	return uuid, NewRedisRegistrar()
}

func TestRegisteredIsRegistered(t *testing.T) {
	uuid, r := redisTestSetup()
	r.Register(uuid)

	if !r.IsRegistered(uuid) {
		t.Fatalf("%s should be registered", uuid)
	}
}

func TestUnregisteredIsNotRegistered(t *testing.T) {
	uuid, r := redisTestSetup()

	if r.IsRegistered(uuid) {
		t.Fatalf("%s should not be registered", uuid)
	}
}

func TestUnregisteredErrNotRegistered(t *testing.T) {
	uuid, _ := redisTestSetup()

	if _, err := NewReader(uuid); err != ErrNotRegistered {
		t.Fatalf("NewReader should return ErrNotRegistered")
	}

	if _, err := NewWriter(uuid); err != ErrNotRegistered {
		t.Fatalf("NewWriter should return ErrNotRegistered")
	}
}

func TestRegisteredNoError(t *testing.T) {
	uuid, r := redisTestSetup()
	r.Register(uuid)

	if _, err := NewReader(uuid); err != nil {
		t.Fatalf("NewReader shouldn't return an error")
	}

	if _, err := NewWriter(uuid); err != nil {
		t.Fatalf("NewWriter shouldn't return an error")
	}
}
