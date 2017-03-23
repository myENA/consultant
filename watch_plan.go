package consultant

import (
	"reflect"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
)

type WatchPlanType string

const (
	// retryInterval is the base retry value
	retryInterval = 5 * time.Second

	// set an upper limit to how long we are prepared to wait for a response from a watched endpoint.
	maxBackOffTime = 180 * time.Second

	WatchPlanTypeService     WatchPlanType = "service"
	WatchPlanTypeServiceList WatchPlanType = "service-list"
	WatchPlanTypeKey         WatchPlanType = "key"
	WatchPlanTypeKeyList     WatchPlanType = "key-list"
	WatchPlanTypeEvent       WatchPlanType = "event"
)

// WatchAction is used to watch for a diff
type WatchAction func(*WatchPlan) (uint64, interface{}, error)

// WatchHandler is used to handle new data
type WatchHandler func(uint64, interface{})

// WatchPlan is the parsed version of a watch specification. A watch provides
// the details of a query, which generates a view into the Consul data store.
// This view is watched for changes and a handler is invoked to take any
// appropriate actions.
// Simplified version based on hashicorp/consul/watch
type WatchPlan struct {
	Type WatchPlanType

	Action  WatchAction
	Handler WatchHandler

	StopOnError bool

	lastIndex  uint64
	lastResult interface{}

	stop     bool
	stopCh   chan struct{}
	stopLock sync.Mutex
}

// create the watch plan
func NewWatchPlan() *WatchPlan {
	return &WatchPlan{
		stopCh: make(chan struct{}),
	}
}

// Stop stops a running watchplan
func (p *WatchPlan) Stop() error {
	p.stopLock.Lock()
	defer p.stopLock.Unlock()

	var code WatchPlanErrorCode

	if p.stop {
		code = WatchPlanErrorAlreadyStopped
	} else {
		p.stop = true
		p.stopCh <- struct{}{}
		close(p.stopCh)
	}

	return getWatchPlanError(code)
}

// shouldStop indicates a watcher should be stopped
func (p *WatchPlan) shouldStop() bool {
	select {
	case <-p.stopCh:
		return true
	default:
		return false
	}
}

// NewServiceWatchPlan builds a WatchPlan for a specific service
func NewServiceWatchPlan(c *api.Client, name string, tag string, passingOnly bool, queryOptions api.QueryOptions, handler WatchHandler) (*WatchPlan, error) {
	wp := NewWatchPlan()
	wp.Type = WatchPlanTypeService
	wp.Handler = handler

	options := &queryOptions

	wp.Action = func(p *WatchPlan) (uint64, interface{}, error) {
		options.WaitIndex = p.lastIndex
		nodes, meta, err := c.Health().Service(name, tag, passingOnly, options)
		if debug {
			log.Printf("[watchplan] In ServiceWatch/Action: service=%s, li=%d, err=%s", name, meta.LastIndex, err)
		}
		if err != nil {
			return 0, nil, err
		}
		return meta.LastIndex, nodes, err
	}

	return wp, nil
}

// NewServiceListPlan builds a watch plan to list of available services
func NewServiceListPlan(c *api.Client, queryOptions api.QueryOptions, handler WatchHandler) (*WatchPlan, error) {
	wp := NewWatchPlan()
	wp.Type = WatchPlanTypeServiceList
	wp.Handler = handler

	options := &queryOptions

	wp.Action = func(p *WatchPlan) (uint64, interface{}, error) {
		options.WaitIndex = p.lastIndex
		services, meta, err := c.Catalog().Services(options)
		if err != nil {
			return 0, nil, err
		}
		return meta.LastIndex, services, err
	}
	return wp, nil
}

// NewKeyPlan builds a WatchPlan for a particular key
func NewKeyPlan(c *api.Client, key string, queryOptions api.QueryOptions, handler WatchHandler) (*WatchPlan, error) {
	wp := NewWatchPlan()
	wp.Type = WatchPlanTypeKey
	wp.Handler = handler

	options := &queryOptions

	wp.Action = func(p *WatchPlan) (uint64, interface{}, error) {
		options.WaitIndex = p.lastIndex
		kv, qm, err := c.KV().Get(key, options)
		if err != nil {
			return 0, nil, err
		}
		if nil == kv {
			return 0, nil, getWatchPlanError(WatchPlanErrorKeyNotFound)
		}

		return qm.LastIndex, kv, err
	}

	return wp, nil
}

// NewKeyListPlan builds a WatchPlan for a particular key prefix
func NewKeyListPlan(c *api.Client, prefix string, queryOptions api.QueryOptions, handler WatchHandler) (*WatchPlan, error) {
	wp := NewWatchPlan()
	wp.Type = WatchPlanTypeKeyList
	wp.Handler = handler

	options := &queryOptions

	wp.Action = func(p *WatchPlan) (uint64, interface{}, error) {
		options.WaitIndex = p.lastIndex
		kvps, qm, err := c.KV().List(prefix, options)
		if err != nil {
			return 0, nil, err
		}
		return qm.LastIndex, kvps, err
	}
	return wp, nil
}

// NewEventPlan builds a WatchPlan for events optionally filtering my name if specified
func NewEventPlan(c *api.Client, name string, queryOptions api.QueryOptions, handler WatchHandler) (*WatchPlan, error) {
	wp := NewWatchPlan()
	wp.Type = WatchPlanTypeEvent
	wp.Handler = handler

	options := &queryOptions

	wp.Action = func(p *WatchPlan) (uint64, interface{}, error) {
		options.WaitIndex = p.lastIndex
		eventClient := c.Event()
		events, qm, err := eventClient.List(name, options)
		if err != nil {
			return 0, nil, err
		}

		// Prune to only the new events
		for i := 0; i < len(events); i++ {
			if eventClient.IDToIndex(events[i].ID) == p.lastIndex {
				events = events[i+1:]
				break
			}
		}
		return qm.LastIndex, events, err
	}
	return wp, nil
}

// RunWatchPlan is used to run a watch plan - the instance should have a valid client.
// The WatchPlan's Handler() is triggered when change is detected on the watched endpoint.
func RunWatchPlan(wp *WatchPlan) error {

	// check if plan has been stopped or is stopping
	if wp.shouldStop() {
		return getWatchPlanError(WatchPlanErrorAlreadyStopped)
	}

	// Loop until we are canceled
	failures := 0
OUTER:
	for !wp.shouldStop() {
		// Invoke the handler
		index, result, err := wp.Action(wp)

		// Check if we should terminate since the function
		// could have blocked for a while
		if wp.shouldStop() {
			return nil
		}

		// Handle an error in the watch function
		if err != nil {
			if wp.StopOnError {
				return err
			}

			// Perform an quadratic backoff
			failures++
			retry := retryInterval * time.Duration(failures*failures)
			if retry > maxBackOffTime {
				retry = maxBackOffTime
			}
			log.Printf("[watchplan] Watch (type: %s) errored: %v, retry in %v",
				wp.Type, err, retry)
			select {
			case <-time.After(retry):
				continue OUTER
			case <-wp.stopCh:
				return err
			}
		}

		// Clear the failures
		failures = 0

		// If the index is unchanged do nothing
		if index == wp.lastIndex {
			continue
		}

		// Update the index, look for change
		oldIndex := wp.lastIndex
		wp.lastIndex = index

		// discard the first change (from an unknown state)
		if oldIndex == 0 {
			continue
		}

		// make damn sure we have a change
		if reflect.DeepEqual(wp.lastResult, result) {
			continue
		}

		// Handle the updated result
		wp.lastResult = result
		if wp.Handler != nil {
			wp.Handler(index, result)
		}
	}

	return nil
}
