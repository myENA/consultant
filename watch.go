package consultant

import (
	"github.com/hashicorp/consul/api/watch"
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

// WatchKeyHandler wraps the creation of a "key" plan, additionally assigning an index handler func
func WatchKeyHandler(key string, stale bool, token, datacenter string, handler watch.HandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchKey(key, stale, token, datacenter); wp != nil {
		wp.Handler = handler
	}
	return
}

// WatchKeyHybridHandler wraps the creation of a "key" plan, additionally assigning hybrid (hash) handler func
func WatchKeyHybridHandler(key string, stale bool, token, datacenter string, hybridHandler watch.HybridHandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchKey(key, stale, token, datacenter); wp != nil {
		wp.HybridHandler = hybridHandler
	}
	return
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

// WatchKeyPrefixHandler wraps the creation of a "keyprefix" plan, additionally assigning  an index handler func
func WatchKeyPrefixHandler(prefix string, stale bool, token, datacenter string, handler watch.HandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchKeyPrefix(prefix, stale, token, datacenter); wp != nil {
		wp.Handler = handler
	}
	return
}

// WatchKeyPrefixHybridHandler wraps the creation of a "keyprefix" plan, additionally assigning a hash handler func
func WatchKeyPrefixHybridHandler(prefix string, stale bool, token, datacenter string, handler watch.HybridHandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchKeyPrefix(prefix, stale, token, datacenter); wp != nil {
		wp.HybridHandler = handler
	}
	return
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

// WatchServicesHandler wraps the creation of a "services" plan, additionally assigning an index handler
func WatchServicesHandler(stale bool, token, datacenter string, handler watch.HandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchServices(stale, token, datacenter); wp != nil {
		wp.Handler = handler
	}
	return
}

// WatchServicesHybridHandler wraps the creation of a "services" plan, additionally assigning a hash handler
func WatchServicesHybridHandler(stale bool, token, datacenter string, handler watch.HybridHandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchServices(stale, token, datacenter); wp != nil {
		wp.HybridHandler = handler
	}
	return
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

// WatchNodesHandler wraps the creation of a "nodes" plan, additionally assigning an index handler
func WatchNodesHandler(stale bool, token, datacenter string, handler watch.HandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchNodes(stale, token, datacenter); wp != nil {
		wp.Handler = handler
	}
	return
}

// WatchNodesHybridHandler wraps the creation of a "nodes" plan, additionally assigning a hash handler
func WatchNodesHybridHandler(stale bool, token, datacenter string, handler watch.HybridHandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchNodes(stale, token, datacenter); wp != nil {
		wp.HybridHandler = handler
	}
	return
}

func WatchServiceMultipleTags(service string, tags []string, passingOnly, stale bool, token, datacenter string) (*watch.Plan, error) {
	return watch.Parse(map[string]interface{}{
		"type":        "service",
		"service":     service,
		"tag":         tags,
		"passingonly": passingOnly,
		"stale":       stale,
		"token":       token,
		"datacenter":  datacenter,
	})
}

// WatchService wraps the creation of a "service" plan
func WatchService(service, tag string, passingOnly, stale bool, token, datacenter string) (*watch.Plan, error) {
	var tags []string
	if tag != "" {
		tags = []string{tag}
	}
	return WatchServiceMultipleTags(service, tags, passingOnly, stale, token, datacenter)
}

// WatchServiceHandler wraps the creation of a "service" plan, additionally setting an index handler
func WatchServiceHandler(service, tag string, passingOnly, stale bool, token, datacenter string, handler watch.HandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchService(service, tag, passingOnly, stale, token, datacenter); wp != nil {
		wp.Handler = handler
	}
	return
}

// WatchServiceHybridHandler wraps the creation of a "service" plan, additionally setting a hash handler
func WatchServiceHybridHandler(service, tag string, passingOnly, stale bool, token, datacenter string, handler watch.HybridHandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchService(service, tag, passingOnly, stale, token, datacenter); wp != nil {
		wp.HybridHandler = handler
	}
	return
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

// WatchChecksHandler wraps the creation of a "checks" plan, additionally setting an index handler
func WatchChecksHandler(service, state string, stale bool, token, datacenter string, handler watch.HandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchChecks(service, state, stale, token, datacenter); wp != nil {
		wp.Handler = handler
	}
	return
}

// WatchChecksHybridHandler wraps the creation of a "checks" plan, additionally setting a hash handler
func WatchChecksHybridHandler(service, state string, stale bool, token, datacenter string, handler watch.HybridHandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchChecks(service, state, stale, token, datacenter); wp != nil {
		wp.HybridHandler = handler
	}
	return
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

// WatchEventHandler wraps the creation of an "event" plan, additionally setting an index handler
func WatchEventHandler(name, token, datacenter string, handler watch.HandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchEvent(name, token, datacenter); wp != nil {
		wp.Handler = handler
	}
	return
}

// WatchEventHybridHandler wraps the creation of an "event" plan, additionally setting a hash handler
func WatchEventHybridHandler(name, token, datacenter string, handler watch.HybridHandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchEvent(name, token, datacenter); wp != nil {
		wp.HybridHandler = handler
	}
	return
}

// WatchConnectRoots wraps the creation of a "connect_roots" plan
func WatchConnectRoots(token, datacenter string) (*watch.Plan, error) {
	return watch.Parse(map[string]interface{}{
		"type":       "connect_roots",
		"token":      token,
		"datacenter": datacenter,
	})
}

// WatchConnectRootsHandler wraps the creation of a "connect_roots" plan, additionally setting an index handler
func WatchConnectRootsHandler(token, datacenter string, handler watch.HandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchConnectRoots(token, datacenter); wp != nil {
		wp.Handler = handler
	}
	return
}

// WatchConnectRootsHybridHandler wraps the creation of a "connect_roots" plan, additionally setting a hash handler
func WatchConnectRootsHybridHandler(token, datacenter string, handler watch.HybridHandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchConnectRoots(token, datacenter); wp != nil {
		wp.HybridHandler = handler
	}
	return
}

// WatchConnectLeaf wraps the creation of a "connect_leaf" plan
func WatchConnectLeaf(service, token, datacenter string) (*watch.Plan, error) {
	return watch.Parse(map[string]interface{}{
		"type":       "connect_leaf",
		"service":    service,
		"token":      token,
		"datacenter": datacenter,
	})
}

// WatchConnectLeafHandler wraps the creation of a "connect_leaf" plan, additionally setting an index handler
func WatchConnectLeafHandler(service, token, datacenter string, handler watch.HandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchConnectLeaf(service, token, datacenter); wp != nil {
		wp.Handler = handler
	}
	return
}

// WatchConnectLeafHybridHandler wraps the creation of a "connect_leaf" plan, additionally setting a hash handler
func WatchConnectLeafHybridHandler(service, token, datacenter string, handler watch.HybridHandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchConnectLeaf(service, token, datacenter); wp != nil {
		wp.HybridHandler = handler
	}
	return
}

// WatchAgentService wraps the creation of a "agent_service" plan
func WatchAgentService(serviceID string) (*watch.Plan, error) {
	return watch.Parse(map[string]interface{}{
		"type":       "agent_service",
		"service_id": serviceID,
	})
}

// WatchAgentServiceHandler wraps the creation of a "connect_proxy_config" plan, additionally setting an index handler
func WatchAgentServiceHandler(serviceID string, handler watch.HandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchAgentService(serviceID); wp != nil {
		wp.Handler = handler
	}
	return
}

// WatchAgentServiceHybridHandler wraps the creation of a "connect_proxy_config" plan, additionally setting a hash
// handler
func WatchAgentServiceHybridHandler(serviceID string, handler watch.HybridHandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchAgentService(serviceID); wp != nil {
		wp.HybridHandler = handler
	}
	return
}

// WatchKey will attempt to create a "key" watch plan based on existing client configuration
func (c *Client) WatchKey(key string, stale bool, handler watch.HandlerFunc) (*watch.Plan, error) {
	return WatchKeyHandler(key, stale, c.confToken, c.confDatacenter, handler)
}

// WatchKeyPrefix will attempt to create a "keyprefix" watch plan based on existing client configuration
func (c *Client) WatchKeyPrefix(prefix string, stale bool, handler watch.HandlerFunc) (*watch.Plan, error) {
	return WatchKeyPrefixHandler(prefix, stale, c.confToken, c.confDatacenter, handler)
}

// WatchServices will attempt to create a "services" watch plan based on existing client configuration
func (c *Client) WatchServices(stale bool, handler watch.HandlerFunc) (*watch.Plan, error) {
	return WatchServicesHandler(stale, c.confToken, c.confDatacenter, handler)
}

// WatchNodes will attempt to create a "nodes" watch plan based on existing client configuration
func (c *Client) WatchNodes(stale bool, handler watch.HandlerFunc) (*watch.Plan, error) {
	return WatchNodesHandler(stale, c.confToken, c.confDatacenter, handler)
}

// WatchService will attempt to create a "service" watch plan based on existing client configuration
func (c *Client) WatchService(service, tag string, passingOnly, stale bool, handler watch.HandlerFunc) (*watch.Plan, error) {
	return WatchServiceHandler(service, tag, passingOnly, stale, c.confToken, c.confDatacenter, handler)
}

// WatchChecks will attempt to create a "checks" watch plan based on existing client configuration
func (c *Client) WatchChecks(service, state string, stale bool, handler watch.HandlerFunc) (*watch.Plan, error) {
	return WatchChecksHandler(service, state, stale, c.confToken, c.confDatacenter, handler)
}

// WatchEvent will attempt to create an "event" watch plan based on existing client configuration
func (c *Client) WatchEvent(name string, handler watch.HandlerFunc) (*watch.Plan, error) {
	return WatchEventHandler(name, c.confToken, c.confDatacenter, handler)
}

// WatchConnectRoots will attempt to create a "connect_roots" watch plan based on existing client configuration
func (c *Client) WatchConnectRoots(handler watch.HandlerFunc) (*watch.Plan, error) {
	return WatchConnectRootsHandler(c.confToken, c.confDatacenter, handler)
}

// WatchConnectLeaf will attempt to create a "connect_leaf" watch plan based on existing client configuration
func (c *Client) WatchConnectLeaf(service string, handler watch.HandlerFunc) (*watch.Plan, error) {
	return WatchConnectLeafHandler(service, c.confToken, c.confDatacenter, handler)
}

// WatchAgentService will attempt to create a "connect_proxy_config" watch plan based on existing client configuration
func (c *Client) WatchAgentService(serviceID string, handler watch.HandlerFunc) (*watch.Plan, error) {
	return WatchAgentServiceHandler(serviceID, handler)
}
