package util

import (
	"flag"
	"os"
	"time"

	"github.com/heroku/rollbar"
)

var (
	EnforceHTTPS       = flag.Bool("enforceHttps", os.Getenv("ENFORCE_HTTPS") == "1", "Whether to enforce use of HTTPS.")
	HeartbeatDuration  = flag.Duration("subscribeHeartbeatDuration", time.Millisecond*100, "Heartbeat interval for HTTP stream subscriptions.")
	HttpPort           = flag.String("httpPort", os.Getenv("PORT"), "HTTP port for the server.")
	HttpReadTimeout    = flag.Duration("httpReadTimeout", time.Hour, "Timeout for HTTP request reading")
	HttpWriteTimeout   = flag.Duration("httpWriteTimeout", time.Hour, "Timeout for HTTP request writing")
	RollbarEnvironment = flag.String("rollbarEnvironment", os.Getenv("ROLLBAR_ENVIRONMENT"), "Rollbar Enviornment for this application (development/staging/production).")
	RollbarToken       = flag.String("rollbarToken", os.Getenv("ROLLBAR_TOKEN"), "Rollbar Token for sending issues to Rollbar.")
)

func init() {
	if *RollbarToken != "" {
		rollbar.Token = *RollbarToken
		rollbar.Environment = *RollbarEnvironment
		rollbar.ServerRoot = "github.com/heroku/busl"
	}
}
