package logger

import (
	"github.com/zerodha/logf"
)

var L logf.Logger

func Init(debug bool) {
	opts := logf.Opts{}
	if debug {
		opts.Level = logf.DebugLevel
	} else {
		opts.Level = logf.InfoLevel
	}
	L = logf.New(opts)
}
