package util

import (
	"os"
	"sync"
)

var (
	myHostname   string
	myHostnameMu sync.RWMutex
)

func tryGetHostname() (string, error) {
	var err error
	myHostnameMu.Lock()
	if myHostname != "" {
		hostname := myHostname
		myHostnameMu.Unlock()
		return hostname, nil
	}
	myHostname, err = os.Hostname()
	if err != nil {
		myHostnameMu.Unlock()
		return "", err
	}
	addr := myHostname
	myHostnameMu.Unlock()
	return addr, nil
}

// MyHostName will either return the value / error from os.Hostname(), or the hostname you specified with SetMyHostname
func MyHostname() (string, error) {
	myHostnameMu.RLock()
	if myHostname == "" {
		myHostnameMu.RUnlock()
		return tryGetHostname()
	}
	hostname := myHostname
	myHostnameMu.RUnlock()
	return hostname, nil
}

// SetMyHostname allows you to specifically set the hostname used in various parts of Consultant
func SetMyHostname(hostname string) {
	myHostnameMu.Lock()
	myHostname = hostname
	myHostnameMu.Unlock()
}
