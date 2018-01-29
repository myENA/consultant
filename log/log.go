package log

import (
	"fmt"
	stdlog "log"
	"os"
)

type Logger interface {
	Print(...interface{})
	Printf(string, ...interface{})
}

type DebugLogger interface {
	Logger
	Debug(v ...interface{})
	Debugf(f string, v ...interface{})
}

var (
	log   Logger
	debug bool
)

// create default logger
func init() {
	log = stdlog.New(os.Stderr, "", stdlog.LstdFlags)
}

func SetPackageLogger(logger Logger) {
	log = logger
}

// Debug will enable additional logging
func EnableDebug() {
	debug = true
}

func DisableDebug() {
	debug = false
}

func Print(v ...interface{}) {
	log.Print(v...)
}

func Printf(f string, v ...interface{}) {
	log.Printf(f, v...)
}

func Debug(v ...interface{}) {
	if debug {
		Print(v...)
	}
}

func Debugf(f string, v ...interface{}) {
	if debug {
		Printf(f, v...)
	}
}

type namedLogger struct {
	slug      string
	slugSlice []interface{}
}

// New will return an new portable logger to you
func New(name string) DebugLogger {
	nl := &namedLogger{
		slug:      fmt.Sprintf("[%s] ", name),
		slugSlice: []interface{}{fmt.Sprintf("[%s] ", name)},
	}
	return nl
}

func (l *namedLogger) Print(v ...interface{}) {
	Print(append(l.slugSlice, v...)...)
}

func (l *namedLogger) Printf(f string, v ...interface{}) {
	Printf(fmt.Sprintf("%s%s", l.slug, f), v...)
}

func (l *namedLogger) Debug(v ...interface{}) {
	if debug {
		l.Print(v...)
	}
}

func (l *namedLogger) Debugf(f string, v ...interface{}) {
	if debug {
		l.Printf(f, v...)
	}
}
