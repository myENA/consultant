package service

import (
	"errors"
	"fmt"
	"github.com/hashicorp/consul/api"
	logger "github.com/myENA/consultant/log"
	"github.com/myENA/consultant/util"
	"strings"
	"sync"
)

var (
	log logger.Logger

	defaultClient   *api.Client
	defaultClientMu sync.RWMutex
)

func init() {
	var err error
	log = logger.New("service")
	defaultClient, err = api.NewClient(api.DefaultConfig())
	if err != nil {
		log.Printf("Error: Unable to create default Consul client for service package: %s", err)
	}
}

func apiClient(client *api.Client) *api.Client {
	if client != nil {
		return client
	}
	defaultClientMu.RLock()
	if defaultClient == nil {
		log.Print("Error: No default client is set for service package")
	}
	c := defaultClient
	defaultClientMu.RUnlock()
	return c
}

func SetDefaultClient(client *api.Client) {
	if client == nil {
		return
	}
	defaultClientMu.Lock()
	defaultClient = client
	defaultClientMu.Unlock()
}

// GenerateID is a convenience method to generate service ID's with.
//
// The structure of the ID will always be:
//		"{name}-{tail}"
// where "{tail}" is either a 12 character pseudo-random string (if "random" is true), or a lower-cased version of the
// hostname running this process as determined by util.MyHostname()
//
// Error only ever happens when "name" is empty, or if random is false and "os.Hostname()" is unable to return anything
func GenerateID(name string, random bool) (string, error) {
	if name == "" {
		return "", errors.New("name cannot be empty")
	}
	var tail string
	if random {
		tail = util.RandStr(12)
	} else {
		myHost, err := util.MyHostname()
		if err != nil {
			return "", fmt.Errorf("unable to find local hostname for service id: %s", err)
		}
		tail = strings.ToLower(myHost)
	}
	return fmt.Sprintf("%s-%s", name, tail), nil
}
