package consultant

import (
	"fmt"
	"github.com/hashicorp/consul/watch"
)

// A config object that implement this interface will have Update() called on any changes in the initialized prefix
// It is up to the implementation to ensure that Updates() are done in a thread-safe way
type Configurator interface {
	Update(uint64, interface{})
}

// InitConfigurator initializes the passed config object.
// Current values are read from the consul KV store and stored in 'config' (passed by reference).
// A watch plan is set up to call Update() whenever the KV prefix is changed.
func (c *Client) InitConfigurator(config Configurator, prefix string) (*watch.Plan, error) {

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
