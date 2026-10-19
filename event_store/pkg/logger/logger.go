package logger

import (
	"os"

	"github.com/zerodha/logf"
)

var L logf.Logger

func Init() {
	L = New(os.Stderr)
}

// New logs logfmt lines to out, coloured only when out is a terminal, so no
// colour codes reach a Log when Vector ships the output (docs/adr/0011).
func New(out *os.File) logf.Logger {
	opts := logf.Opts{
		Writer:          out,
		EnableColor:     isTerminal(out),
		EnableCaller:    true,
		TimestampFormat: "2006-01-02 15:04:05 MST",
	}
	if os.Getenv("DEBUG") == "true" {
		opts.Level = logf.DebugLevel
	} else {
		opts.Level = logf.InfoLevel
	}
	return logf.New(opts)
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
