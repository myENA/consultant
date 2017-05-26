package consultant

import (
	"fmt"
	"github.com/hashicorp/consul/watch"
	"strings"
)

// A config object that implement this interface will have Update() called on any changes in the initialized prefix
// It is up to the implementation to ensure that Updates() are done in a thread-safe way
type Configurator interface {
	Update(uint64, interface{})
}

// ConfigureKVPrefix initializes the passed config object for updates under a given prefix
// Current values are read from the consul KV store and stored in 'config' (passed by reference).
// A watch plan is set up to call Update() whenever the KV prefix is changed.
func (c *Client) ConfigureKVPrefix(config Configurator, prefix string) (*watch.Plan, error) {

	kvps, _, err := c.KV().List(prefix, nil)
	if err != nil {
		return nil, fmt.Errorf("Trouble getting the KVs under: %s", prefix)
	}
	config.Update(0, kvps)

	wp, err := c.WatchKeyPrefix(prefix, true, config.Update)
	if err != nil {
		return nil, fmt.Errorf("Trouble building the watch plan: %s", err)
	}

	go func() {
		err := wp.Run(c.config.Address)
		if err != nil {
			fmt.Fprintf(wp.LogOutput, "Watch plan failed for prefix: %s", prefix)
		}
	}()

	return wp, nil
}

// ConfigureServices iniitializes the passed config object for updates about a services.
// A random passing service populate the config.
// When anything changes for those services config.Update() is notified and config is updated.
func (c *Client) ConfigureService(config Configurator, service string, tag string) (*watch.Plan, error) {
	var wp *watch.Plan
	var err error

	se, _, err := c.PickService(service, tag, true, nil)
	if err != nil {
		return nil, fmt.Errorf("Trouble finding a passing service for %s (tag=%s)",service,tag)
	}
	
}
