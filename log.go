package consultant

import (
	"github.com/myENA/consultant/log"
)

// Logger is here for compatibility and will be removed in the future
type Logger interface {
	log.Logger
}

func SetPackageLogger(logger Logger) {
	log.SetPackageLogger(logger)
}

// Debug will enable additional logging
func Debug() {
	log.EnableDebug()
}

func DisableDebug() {
	log.DisableDebug()
}
