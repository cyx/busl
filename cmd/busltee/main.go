package main

import (
	"errors"
	"fmt"
	"os"

	_ "github.com/heroku/busl/Godeps/_workspace/src/github.com/heroku/log-shuttle"
	flag "github.com/heroku/busl/Godeps/_workspace/src/github.com/ogier/pflag"
	"github.com/heroku/busl/busltee"
)

func main() {
	conf, err := parseFlags()
	if err != nil {
		usage()
		os.Exit(1)
	}

	busltee.OpenLogs(conf.LogFile, conf.LogPrefix)
	defer busltee.CloseLogs()

	if exitCode := busltee.Run(conf.URL, conf.Args, conf); exitCode != 0 {
		os.Exit(exitCode)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s [OPTIONS] <url> -- <command>\n", os.Args[0])
	flag.PrintDefaults()
}

func parseFlags() (*busltee.Config, error) {
	conf := &busltee.Config{}

	// Connection related flags
	flag.BoolVarP(&conf.Insecure, "insecure", "k", false, "allows insecure SSL connections")
	flag.IntVar(&conf.Retry, "retry", 5, "max retries for connect timeout errors")
	flag.Float64Var(&conf.Timeout, "connect-timeout", 1, "max number of seconds to connect to busl URL")

	// Logging related flags
	flag.StringVar(&conf.LogPrefix, "log-prefix", "", "log prefix")
	flag.StringVar(&conf.LogFile, "log-file", "", "log file")

	if flag.Parse(); len(flag.Args()) < 2 {
		return nil, errors.New("insufficient args")
	}

	conf.URL = flag.Arg(0)
	conf.Args = flag.Args()[1:]

	return conf, nil
}
