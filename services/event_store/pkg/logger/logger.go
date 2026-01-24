package logger

import (
	"github.com/zerodha/logf"
)

var L logf.Logger

func Init(debug bool) {
	opts := logf.Opts{
		EnableColor:     true,
		EnableCaller:    true,
		TimestampFormat: "2006-01-02 15:04:05 MST",
	}
	if debug {
		opts.Level = logf.DebugLevel
	} else {
		opts.Level = logf.InfoLevel
	}
	L = logf.New(opts)
}
