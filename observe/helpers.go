package observe

import "github.com/hashicorp/consul/watch"

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
	if wp, err = WatchKey(key, stale, token, datacenter); err == nil {
		wp.Handler = handler
	}
	return
}

// WatchKeyHybridHandler wraps the creation of a "key" plan, additionally assigning hybrid (hash) handler func
func WatchKeyHybridHandler(key string, stale bool, token, datacenter string, hybridHandler watch.HybridHandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchKey(key, stale, token, datacenter); err == nil {
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
	if wp, err = WatchKeyPrefix(prefix, stale, token, datacenter); wp == nil {
		wp.Handler = handler
	}
	return
}

// WatchKeyPrefixHybridHandler wraps the creation of a "keyprefix" plan, additionally assigning a hash handler func
func WatchKeyPrefixHybridHandler(prefix string, stale bool, token, datacenter string, handler watch.HybridHandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchKeyPrefix(prefix, stale, token, datacenter); wp == nil {
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
	if wp, err = WatchServices(stale, token, datacenter); err == nil {
		wp.Handler = handler
	}
	return
}

// WatchServicesHybridHandler wraps the creation of a "services" plan, additionally assigning a hash handler
func WatchServicesHybridHandler(stale bool, token, datacenter string, handler watch.HybridHandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchServices(stale, token, datacenter); err == nil {
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
	if wp, err = WatchNodes(stale, token, datacenter); err == nil {
		wp.Handler = handler
	}
	return
}

// WatchNodesHybridHandler wraps the creation of a "nodes" plan, additionally assigning a hash handler
func WatchNodesHybridHandler(stale bool, token, datacenter string, handler watch.HybridHandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchNodes(stale, token, datacenter); err == nil {
		wp.HybridHandler = handler
	}
	return
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

// WatchServiceHandler wraps the creation of a "service" plan, additionally setting an index handler
func WatchServiceHandler(service, tag string, passingOnly, stale bool, token, datacenter string, handler watch.HandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchService(service, tag, passingOnly, stale, token, datacenter); err == nil {
		wp.Handler = handler
	}
	return
}

// WatchServiceHybridHandler wraps the creation of a "service" plan, additionally setting a hash handler
func WatchServiceHybridHandler(service, tag string, passingOnly, stale bool, token, datacenter string, handler watch.HybridHandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchService(service, tag, passingOnly, stale, token, datacenter); err == nil {
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
	if wp, err = WatchChecks(service, state, stale, token, datacenter); err == nil {
		wp.Handler = handler
	}
	return
}

// WatchChecksHybridHandler wraps the creation of a "checks" plan, additionally setting a hash handler
func WatchChecksHybridHandler(service, state string, stale bool, token, datacenter string, handler watch.HybridHandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchChecks(service, state, stale, token, datacenter); err == nil {
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
	if wp, err = WatchEvent(name, token, datacenter); err == nil {
		wp.Handler = handler
	}
	return
}

// WatchEventHybridHandler wraps the creation of an "event" plan, additionally setting a hash handler
func WatchEventHybridHandler(name, token, datacenter string, handler watch.HybridHandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchEvent(name, token, datacenter); err == nil {
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
	if wp, err = WatchConnectRoots(token, datacenter); err == nil {
		wp.Handler = handler
	}
	return
}

// WatchConnectRootsHybridHandler wraps the creation of a "connect_roots" plan, additionally setting a hash handler
func WatchConnectRootsHybridHandler(token, datacenter string, handler watch.HybridHandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchConnectRoots(token, datacenter); err == nil {
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
	if wp, err = WatchConnectLeaf(service, token, datacenter); err == nil {
		wp.Handler = handler
	}
	return
}

// WatchConnectLeafHybridHandler wraps the creation of a "connect_leaf" plan, additionally setting a hash handler
func WatchConnectLeafHybridHandler(service, token, datacenter string, handler watch.HybridHandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchConnectLeaf(service, token, datacenter); err == nil {
		wp.HybridHandler = handler
	}
	return
}

// WatchProxyConfig wraps the creation of a "connect_proxy_config" plan
func WatchProxyConfig(proxyServiceID, token, datacenter string) (*watch.Plan, error) {
	return watch.Parse(map[string]interface{}{
		"type":             "connect_proxy_config",
		"proxy_service_id": proxyServiceID,
		"token":            token,
		"datacenter":       datacenter,
	})
}

// WatchProxyConfigHandler wraps the creation of a "connect_proxy_config" plan, additionally setting an index handler
func WatchProxyConfigHandler(proxyServiceID, token, datacenter string, handler watch.HandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchProxyConfig(proxyServiceID, token, datacenter); err == nil {
		wp.Handler = handler
	}
	return
}

// WatchProxyConfigHybridHandler wraps the creation of a "connect_proxy_config" plan, additionally setting a hash
// handler
func WatchProxyConfigHybridHandler(proxyServiceID, token, datacenter string, handler watch.HybridHandlerFunc) (wp *watch.Plan, err error) {
	if wp, err = WatchProxyConfig(proxyServiceID, token, datacenter); err == nil {
		wp.HybridHandler = handler
	}
	return
}
