package consultant

import (
	"github.com/hashicorp/consul/watch"
	"github.com/myENA/consultant/observe"
)

// WatchKey wraps the creation of a "key" plan
// DEPRECATED
func WatchKey(key string, stale bool, token, datacenter string) (*watch.Plan, error) {
	return observe.WatchKey(key, stale, token, datacenter)
}

// WatchKeyPrefix wraps the creation of a "keyprefix" plan
// DEPRECATED
func WatchKeyPrefix(prefix string, stale bool, token, datacenter string) (*watch.Plan, error) {
	return observe.WatchKeyPrefix(prefix, stale, token, datacenter)
}

// WatchServices wraps the creation of a "services" plan
// DEPRECATED
func WatchServices(stale bool, token, datacenter string) (*watch.Plan, error) {
	return observe.WatchServices(stale, token, datacenter)
}

// WatchNodes wraps the creation of a "nodes" plan
// DEPRECATED
func WatchNodes(stale bool, token, datacenter string) (*watch.Plan, error) {
	return observe.WatchNodes(stale, token, datacenter)
}

// WatchService wraps the creation of a "service" plan
// DEPRECATED
func WatchService(service, tag string, passingOnly, stale bool, token, datacenter string) (*watch.Plan, error) {
	return observe.WatchService(service, tag, passingOnly, stale, token, datacenter)
}

// WatchChecks wraps the creation of a "checks" plan
// DEPRECATED
func WatchChecks(service, state string, stale bool, token, datacenter string) (*watch.Plan, error) {
	return observe.WatchChecks(service, state, stale, token, datacenter)
}

// WatchEvent wraps the creation of an "event" plan
// DEPRECATED
func WatchEvent(name, token, datacenter string) (*watch.Plan, error) {
	return observe.WatchEvent(name, token, datacenter)
}

// WatchKey will attempt to create a "key" watch plan based on existing client configuration
func (c *Client) WatchKey(key string, stale bool, handler watch.HandlerFunc) (*watch.Plan, error) {
	return observe.WatchKeyHandler(key, stale, c.config.Token, c.config.Datacenter, handler)
}

// WatchKeyPrefix will attempt to create a "keyprefix" watch plan based on existing client configuration
func (c *Client) WatchKeyPrefix(prefix string, stale bool, handler watch.HandlerFunc) (*watch.Plan, error) {
	return observe.WatchKeyPrefixHandler(prefix, stale, c.config.Token, c.config.Datacenter, handler)
}

// WatchServices will attempt to create a "services" watch plan based on existing client configuration
func (c *Client) WatchServices(stale bool, handler watch.HandlerFunc) (*watch.Plan, error) {
	return observe.WatchServicesHandler(stale, c.config.Token, c.config.Datacenter, handler)
}

// WatchNodes will attempt to create a "nodes" watch plan based on existing client configuration
func (c *Client) WatchNodes(stale bool, handler watch.HandlerFunc) (*watch.Plan, error) {
	return observe.WatchNodesHandler(stale, c.config.Token, c.config.Datacenter, handler)
}

// WatchService will attempt to create a "service" watch plan based on existing client configuration
func (c *Client) WatchService(service, tag string, passingOnly, stale bool, handler watch.HandlerFunc) (*watch.Plan, error) {
	return observe.WatchServiceHandler(service, tag, passingOnly, stale, c.config.Token, c.config.Datacenter, handler)
}

// WatchChecks will attempt to create a "checks" watch plan based on existing client configuration
func (c *Client) WatchChecks(service, state string, stale bool, handler watch.HandlerFunc) (*watch.Plan, error) {
	return observe.WatchChecksHandler(service, state, stale, c.config.Token, c.config.Datacenter, handler)
}

// WatchEvent will attempt to create an "event" watch plan based on existing client configuration
func (c *Client) WatchEvent(name string, handler watch.HandlerFunc) (*watch.Plan, error) {
	return observe.WatchEventHandler(name, c.config.Token, c.config.Datacenter, handler)
}

// WatchConnectRoots will attempt to create a "connect_roots" watch plan based on existing client configuration
func (c *Client) WatchConnectRoots(handler watch.HandlerFunc) (*watch.Plan, error) {
	return observe.WatchConnectRootsHandler(c.config.Token, c.config.Datacenter, handler)
}

// WatchConnectLeaf will attempt to create a "connect_leaf" watch plan based on existing client configuration
func (c *Client) WatchConnectLeaf(service string, handler watch.HandlerFunc) (*watch.Plan, error) {
	return observe.WatchConnectLeafHandler(service, c.config.Token, c.config.Datacenter, handler)
}

// WatchProxyConfig will attempt to create a "connect_proxy_config" watch plan based on existing client configuration
func (c *Client) WatchProxyConfig(proxyServiceID string, handler watch.HandlerFunc) (*watch.Plan, error) {
	return observe.WatchProxyConfigHandler(proxyServiceID, c.config.Token, c.config.Datacenter, handler)
}
