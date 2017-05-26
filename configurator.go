package consultant

import (
	"fmt"
	"github.com/hashicorp/consul/watch"
	"github.com/hashicorp/consul/api"
)

// A config object that implements this interface can be initialized from consul's
// KV store or from it's service list and then automatically updated.
// Set up init/watch plans for a KV prefix with `KVPrefixConfigurator()`.
// A service can be similarly observed with `ServiceConfigurator()`.
// It is up to the implementation to ensure that Updates() and use of the config object
// are done in a thread-safe way.
// See the test code for an example implementation.
type Configurator interface {
	Update(uint64, interface{})
}

// KVPrefixConfigurator initializes the passed config object for updates under a given prefix
// Current values are read from the consul KV store and stored in 'config' (passed by reference).
// A watch plan is set up to call Update() whenever the KV prefix is changed.
// Update() should expect to receive a KV pair list (github.com/hashicorp/consul/api: KVPairs)
func (c *Client) KVPrefixConfigurator(config Configurator, prefix string) (*watch.Plan, error) {

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

// ConfigureServices initializes the passed config object for updates about a services.
// A random passing service populate the config.
// When anything changes for those services config.Update() is notified and config is updated.
// Update() should expect to receive a service entry list (github.com/hashicorp/consul/api: []*ServiceEntry)
func (c *Client) ServiceConfigurator(config Configurator, service string, tag string,
	passingOnly bool, options *api.QueryOptions) (*watch.Plan, error) {

	seList, _, err := c.Health().Service(service, tag, passingOnly, nil)
	if err != nil {
		return nil, fmt.Errorf("Trouble finding a passing service for %s (tag=%s)",service,tag)
	}
	config.Update(0,seList)

	wp, err := c.WatchService(service,tag, passingOnly, true, config.Update)
	if err != nil {
		return nil, fmt.Errorf("Trouble building the watch plan: %s", err)
	}

	go func() {
		err := wp.Run(c.config.Address)
		if err != nil {
			fmt.Fprintf(wp.LogOutput, "Watch plan failed for service: %s", service)
		}
	}()

	return wp, nil

}
