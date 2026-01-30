package logger

import (
	"os"

	"github.com/zerodha/logf"
)

var L logf.Logger

func Init() {
	opts := logf.Opts{
		EnableColor:     true,
		EnableCaller:    true,
		TimestampFormat: "2006-01-02 15:04:05 MST",
	}
	debug := os.Getenv("DEBUG") == "true"
	if debug {
		opts.Level = logf.DebugLevel
	} else {
		opts.Level = logf.InfoLevel
	}
	L = logf.New(opts)
}
