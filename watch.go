package consultant

import (
	"github.com/hashicorp/consul/watch"
)

// WatchKey wraps the creation of a "key" plan
func WatchKey(key string, stale bool, token, datacenter string) (*watch.Plan, error) {
	return watch.Parse(map[string]interface{}{
		"type":       "key",
		"key":        key,
		"stale":      stale,
		"token":      token,
		"datacenter": datacenter,
	})
}

// WatchKeyPrefix wraps the creation of a "keyprefix" plan
func WatchKeyPrefix(prefix string, stale bool, token, datacenter string) (*watch.Plan, error) {
	return watch.Parse(map[string]interface{}{
		"type":       "keyprefix",
		"prefix":     prefix,
		"stale":      stale,
		"token":      token,
		"datacenter": datacenter,
	})
}

// WatchServices wraps the creation of a "services" plan
func WatchServices(stale bool, token, datacenter string) (*watch.Plan, error) {
	return watch.Parse(map[string]interface{}{
		"type":       "services",
		"stale":      stale,
		"token":      token,
		"datacenter": datacenter,
	})
}

// WatchNodes wraps the creation of a "nodes" plan
func WatchNodes(stale bool, token, datacenter string) (*watch.Plan, error) {
	return watch.Parse(map[string]interface{}{
		"type":       "nodes",
		"stale":      stale,
		"token":      token,
		"datacenter": datacenter,
	})
}

// WatchService wraps the creation of a "service" plan
func WatchService(service, tag string, passingOnly, stale bool, token, datacenter string) (*watch.Plan, error) {
	return watch.Parse(map[string]interface{}{
		"type":        "service",
		"service":     service,
		"tag":         tag,
		"passingonly": passingOnly,
		"stale":       stale,
		"token":       token,
		"datacenter":  datacenter,
	})
}

// WatchChecks wraps the creation of a "checks" plan
func WatchChecks(service, state string, stale bool, token, datacenter string) (*watch.Plan, error) {
	return watch.Parse(map[string]interface{}{
		"type":       "checks",
		"service":    service,
		"state":      state,
		"stale":      stale,
		"token":      token,
		"datacenter": datacenter,
	})
}

// WatchEvent wraps the creation of an "event" plan
func WatchEvent(name, token, datacenter string) (*watch.Plan, error) {
	return watch.Parse(map[string]interface{}{
		"type":       "event",
		"name":       name,
		"token":      token,
		"datacenter": datacenter,
	})
}

// WatchKey will attempt to create a "key" watch plan based on an existing client configuration
func (c *Client) WatchKey(key string, stale bool, handler watch.HandlerFunc) (*watch.Plan, error) {
	wp, err := WatchKey(key, stale, c.config.Token, c.config.Datacenter)
	if err != nil {
		return nil, err
	}

	wp.Handler = handler

	return wp, nil
}

// WatchKeyPrefix will attempt to create a "keyprefix" watch plan based on an existing client configuration
func (c *Client) WatchKeyPrefix(prefix string, stale bool, handler watch.HandlerFunc) (*watch.Plan, error) {
	wp, err := WatchKeyPrefix(prefix, stale, c.config.Token, c.config.Datacenter)
	if err != nil {
		return nil, err
	}

	wp.Handler = handler

	return wp, nil
}

// WatchServices will attempt to create a "services" watch plan based on an existing client configuration
func (c *Client) WatchServices(stale bool, handler watch.HandlerFunc) (*watch.Plan, error) {
	wp, err := WatchServices(stale, c.config.Token, c.config.Datacenter)
	if err != nil {
		return nil, err
	}

	wp.Handler = handler

	return wp, nil
}

// WatchNodes will attempt to create a "nodes" watch plan based on an existing client configuration
func (c *Client) WatchNodes(stale bool, handler watch.HandlerFunc) (*watch.Plan, error) {
	wp, err := WatchNodes(stale, c.config.Token, c.config.Datacenter)
	if err != nil {
		return nil, err
	}

	wp.Handler = handler

	return wp, nil
}

// WatchService will attempt to create a "service" watch plan based on an existing client configuration
func (c *Client) WatchService(service, tag string, passingOnly, stale bool, handler watch.HandlerFunc) (*watch.Plan, error) {
	wp, err := WatchService(service, tag, passingOnly, stale, c.config.Token, c.config.Datacenter)
	if err != nil {
		return nil, err
	}

	wp.Handler = handler

	return wp, nil
}

// WatchChecks will attempt to create a "checks" watch plan based on an existing client configuration
func (c *Client) WatchChecks(service, state string, stale bool, handler watch.HandlerFunc) (*watch.Plan, error) {
	wp, err := WatchChecks(service, state, stale, c.config.Token, c.config.Datacenter)
	if err != nil {
		return nil, err
	}

	wp.Handler = handler

	return wp, nil
}

// WatchEvent will attempt to create an "event" watch plan based on an existing client configuration
func (c *Client) WatchEvent(name string, handler watch.HandlerFunc) (*watch.Plan, error) {
	wp, err := WatchEvent(name, c.config.Token, c.config.Datacenter)
	if err != nil {
		return nil, err
	}

	wp.Handler = handler

	return wp, nil
}
