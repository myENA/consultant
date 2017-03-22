package consulCandidate

import (
	"fmt"
	"github.com/myENA/go-stdlogger"
	stdlog "log"
	"os"
)

// Accept any logger that implements the core log functions
var log stdlogger.StdLogger

var debug bool

// create default logger
func init() {
	log = stdlog.New(os.Stderr, "", stdlog.LstdFlags)
}

// SetPackageLogger allows you to override the default package logger with your own
func SetPackageLogger(logger stdlogger.StdLogger) {
	log = logger
}

// Debug will enable additional logging
func Debug() {
	debug = true
}

func logPrintf(c *Candidate, format string, v ...interface{}) {
	log.Printf(fmt.Sprintf("[candidate-%s] %s", c.id, format), v...)
}

func logPrint(c *Candidate, v ...interface{}) {
	log.Print(append([]interface{}{fmt.Sprintf("[candidate-%s]", c.id)}, v...)...)
}

func logPrintln(c *Candidate, v ...interface{}) {
	log.Println(append([]interface{}{fmt.Sprintf("[candidate-%s]", c.id)}, v...)...)
}

func logFatalf(c *Candidate, format string, v ...interface{}) {
	log.Fatalf(fmt.Sprintf("[candidate-%s] %s", c.id, format), v...)
}

func logFatal(c *Candidate, v ...interface{}) {
	log.Fatal(append([]interface{}{fmt.Sprintf("[candidate-%s]", c.id)}, v...)...)
}

func logFatalln(c *Candidate, v ...interface{}) {
	log.Fatalln(append([]interface{}{fmt.Sprintf("[candidate-%s]", c.id)}, v...)...)
}

func logPanicf(c *Candidate, format string, v ...interface{}) {
	log.Panicf(fmt.Sprintf("[candidate-%s] %s", c.id, format), v...)
}

func logPanic(c *Candidate, v ...interface{}) {
	log.Panic(append([]interface{}{fmt.Sprintf("[candidate-%s]", c.id)}, v...)...)
}

func logPanicln(c *Candidate, v ...interface{}) {
	log.Panicln(append([]interface{}{fmt.Sprintf("[candidate-%s]", c.id)}, v...)...)
}
