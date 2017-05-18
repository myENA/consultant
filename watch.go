package consultant

import (
	"github.com/hashicorp/consul/watch"
)

func WatchKey(key string, stale bool, token, datacenter string) (*watch.Plan, error) {
	return watch.Parse(map[string]interface{}{
		"type":       "key",
		"key":        key,
		"stale":      stale,
		"token":      token,
		"datacenter": datacenter,
	})
}

func WatchKeyPrefix(prefix string, stale bool, token, datacenter string) (*watch.Plan, error) {
	return watch.Parse(map[string]interface{}{
		"type":       "keyprefix",
		"prefix":     prefix,
		"stale":      stale,
		"token":      token,
		"datacenter": datacenter,
	})
}

func WatchServices(stale bool, token, datacenter string) (*watch.Plan, error) {
	return watch.Parse(map[string]interface{}{
		"type":       "services",
		"stale":      stale,
		"token":      token,
		"datacenter": datacenter,
	})
}

func WatchNodes(stale bool, token, datacenter string) (*watch.Plan, error) {
	return watch.Parse(map[string]interface{}{
		"type":       "nodes",
		"stale":      stale,
		"token":      token,
		"datacenter": datacenter,
	})
}

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
	wp, err := WatchKey(key, stale, c.conf.Token, c.conf.Datacenter)
	if nil != err {
		return nil, err
	}

	wp.Handler = handler

	return wp, nil
}

// WatchKeyPrefix will attempt to create a "keyprefix" watch plan based on an existing client configuration
func (c *Client) WatchKeyPrefix(prefix string, stale bool, handler watch.HandlerFunc) (*watch.Plan, error) {
	wp, err := WatchKeyPrefix(prefix, stale, c.conf.Token, c.conf.Datacenter)
	if nil != err {
		return nil, err
	}

	wp.Handler = handler

	return wp, nil
}

// WatchServices will attempt to create a "services" watch plan based on an existing client configuration
func (c *Client) WatchServices(stale bool, handler watch.HandlerFunc) (*watch.Plan, error) {
	wp, err := WatchServices(stale, c.conf.Token, c.conf.Datacenter)
	if nil != err {
		return nil, err
	}

	wp.Handler = handler

	return wp, nil
}

// WatchService will attempt to create a "service" watch plan based on an existing client configuration
func (c *Client) WatchService(service, tag string, passingOnly, stale bool, handler watch.HandlerFunc) (*watch.Plan, error) {
	wp, err := WatchService(service, tag, passingOnly, stale, c.conf.Token, c.conf.Datacenter)
	if nil != err {
		return nil, err
	}

	wp.Handler = handler

	return wp, nil
}

// WatchChecks will attempt to create a "checks" watch plan based on an existing client configuration
func (c *Client) WatchChecks(service, state string, stale bool, handler watch.HandlerFunc) (*watch.Plan, error) {
	wp, err := WatchChecks(service, state, stale, c.conf.Token, c.conf.Datacenter)
	if nil != err {
		return nil, err
	}

	wp.Handler = handler

	return wp, nil
}

// WatchEvent will attempt to create an "event" watch plan based on an existing client configuration
func (c *Client) WatchEvent(name string, handler watch.HandlerFunc) (*watch.Plan, error) {
	wp, err := WatchEvent(name, c.conf.Token, c.conf.Datacenter)
	if nil != err {
		return nil, err
	}

	wp.Handler = handler

	return wp, nil
}
