package broker

import (
	"github.com/heroku/busl/Godeps/_workspace/src/github.com/garyburd/redigo/redis"
	"github.com/heroku/busl/util"
)

func Register(channelName string) (err error) {
	conn := redisPool.Get()
	defer conn.Close()

	channel := channel(channelName)

	_, err = conn.Do("SETEX", channel.id(), redisChannelExpire, make([]byte, 0))
	if err != nil {
		util.CountWithData("RedisRegistrar.Register.error", 1, "error=%s", err)
		return
	}
	return
}

func IsRegistered(channelName string) (registered bool) {
	conn := redisPool.Get()
	defer conn.Close()

	channel := channel(channelName)

	exists, err := redis.Bool(conn.Do("EXISTS", channel.id()))
	if err != nil {
		util.CountWithData("RedisRegistrar.IsRegistered.error", 1, "error=%s", err)
		return false
	}

	return exists
}
