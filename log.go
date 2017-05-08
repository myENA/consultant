package consultant

import (
	"github.com/myENA/go-stdlogger"
	stdLog "log"
	"os"
)

// Accept any logger that implements the core log functions
var log stdlogger.StdLogger

var debug bool

// create default logger
func init() {
	log = stdLog.New(os.Stderr, "", stdLog.LstdFlags)
}

// SetPackageLogger allows you to override the default package logger with your own
func SetPackageLogger(logger stdlogger.StdLogger) {
	log = logger
}

// Debug will enable additional logging
func Debug() {
	debug = true
}
