package consultant

import (
	stdLog "log"
	"os"
)

type Logger interface {
	Print(...interface{})
	Printf(string, ...interface{})
}

// Accept any logger that implements the core log functions
var (
	log Logger

	debug bool
)

// create default logger
func init() {
	log = stdLog.New(os.Stderr, "", stdLog.LstdFlags)
}

// SetPackageLogger allows you to override the default package logger with your own
func SetPackageLogger(logger Logger) {
	log = logger
}

// Debug will enable additional logging
func Debug() {
	debug = true
}

func DisableDebug() {
	debug = false
}
