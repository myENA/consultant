package consultant

import (
	"errors"
	"fmt"
	"log"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/api/watch"

	"math"
	"strconv"
	"strings"
	"sync"
)

// SiblingCallback is the prototype for a callback that can be registered in a SiblingLocator.  It will be called
// whenever a running watcher receives a new service list from Consul, or optionally when a call to Current is made.
//
// "index" will be "math.MaxUint64" when called using "Current"
type SiblingCallback func(index uint64, siblings Siblings)

// Sibling
type Sibling struct {
	Node    api.Node
	Service api.AgentService
	Checks  api.HealthChecks
}

// Siblings is provided to any callbacks
type Siblings []Sibling

// SiblingLocatorConfig is used to construct a SiblingLocator.  All values except ServiceTags are required.
type SiblingLocatorConfig struct {
	ServiceID   string // REQUIRED ID of service you want to find siblings for.  Used to exclude local service from responses
	ServiceName string // REQUIRED name of service

	NodeName    string   // OPTIONAL name of node where service was registered.  Used to exclude local service from responses.  Will use node client is connected to if not defined.
	ServiceTags []string // OPTIONAL tags to require when looking for siblings
	AllowStale  bool     // OPTIONAL allow "stale" values
	Datacenter  string   // OPTIONAL consul datacenter.  Will use value from Client if left blank
	Token       string   // OPTIONAL consul acl token.  Will use value from Client if left blank
}

// DEPRECATED - This will be replaced in the near future.
//
// SiblingLocator provides a way for a local service to find other services registered in Consul that share it's name
// and tags (if any).
type SiblingLocator struct {
	mu sync.Mutex

	client *Client
	log    log.Logger

	config *SiblingLocatorConfig

	callbacks        map[string]SiblingCallback
	lazyCallbackName uint64

	wp *watch.Plan
}

func NewSiblingLocator(client *Client, config SiblingLocatorConfig) (*SiblingLocator, error) {
	// construct new sibling locator
	sl := &SiblingLocator{
		client:    client,
		config:    &config,
		callbacks: make(map[string]SiblingCallback),
	}

	// verify service id is set
	sl.config.ServiceID = strings.TrimSpace(sl.config.ServiceID)
	if "" == sl.config.ServiceID {
		return nil, errors.New("\"ServiceID\" cannot be empty")
	}

	// verify service name is set
	sl.config.ServiceName = strings.TrimSpace(sl.config.ServiceName)
	if "" == sl.config.ServiceName {
		return nil, errors.New("\"ServiceName\" cannot be empty")
	}

	// verify node name is set, using client node if not
	sl.config.NodeName = strings.TrimSpace(sl.config.NodeName)
	if "" == sl.config.NodeName {
		if n, err := client.Agent().NodeName(); err != nil {
			return nil, fmt.Errorf("node name not provided and error seen while fetching name from local agent: %s", err)
		} else {
			sl.config.NodeName = n
		}
	}

	// verify datacenter is set, using client datacenter if not
	sl.config.Datacenter = strings.TrimSpace(sl.config.Datacenter)
	//if "" == sl.config.Datacenter {
	//	sl.config.Datacenter = sl.client.config.Datacenter
	//}

	// verify token is set, using client token if not
	sl.config.Token = strings.TrimSpace(sl.config.Token)
	//if "" == sl.config.Token {
	//	sl.config.Token = sl.client.config.Token
	//}

	// create copy of tags, if necessary
	if nil == sl.config.ServiceTags || 0 == len(sl.config.ServiceTags) {
		sl.config.ServiceTags = make([]string, 0)
	} else {
		tmp := make([]string, len(sl.config.ServiceTags))
		copy(tmp, sl.config.ServiceTags)
		sl.config.ServiceTags = tmp
	}

	// set up log slugs

	return sl, nil
}

// NewSiblingLocatorWithCatalogService will construct a SiblingLocator from a consul api catalog service struct
func NewSiblingLocatorWithCatalogService(c *Client, cs *api.CatalogService) (*SiblingLocator, error) {
	conf := &SiblingLocatorConfig{
		NodeName:    cs.Node,
		ServiceID:   cs.ServiceID,
		ServiceName: cs.ServiceName,
	}

	if nil == cs.ServiceTags {
		conf.ServiceTags = make([]string, 0)
	} else {
		conf.ServiceTags = make([]string, len(cs.ServiceTags))
		copy(conf.ServiceTags, cs.ServiceTags)
	}

	return NewSiblingLocator(c, *conf)
}

// NewSiblingLocatorWithAgentService will construct a SiblingLocator from a consul api node and agent service struct
func NewSiblingLocatorWithAgentService(c *Client, n *api.Node, as *api.AgentService) (*SiblingLocator, error) {
	conf := &SiblingLocatorConfig{
		NodeName:    n.Node,
		ServiceID:   as.ID,
		ServiceName: as.Service,
	}

	if nil == as.Tags {
		conf.ServiceTags = make([]string, 0)
	} else {
		conf.ServiceTags = make([]string, len(as.Tags))
		copy(conf.ServiceTags, as.Tags)
	}

	return NewSiblingLocator(c, *conf)
}

func (sl *SiblingLocator) AddCallback(name string, cb SiblingCallback) string {
	sl.mu.Lock()

	name = strings.TrimSpace(name)
	if name == "" {
		name = strconv.FormatUint(sl.lazyCallbackName, 10)
		sl.lazyCallbackName++
	}

	sl.callbacks[name] = cb

	sl.mu.Unlock()

	return name
}

func (sl *SiblingLocator) RemoveCallback(name string) {
	sl.mu.Lock()
	delete(sl.callbacks, name)
	sl.mu.Unlock()
}

// StartWatcher will spin up a Consul WatchPlan that watches for other registered services with the same name
// and set of tags.
//
// - passingOnly will limit the response to only registrations deemed "healthy"
func (sl *SiblingLocator) StartWatcher(passingOnly bool) error {
	sl.mu.Lock()
	if sl.wp != nil && !sl.wp.IsStopped() {
		sl.log.Print("watcher is already running")
		sl.mu.Unlock()
		return nil
	}

	var err error
	var tag string

	if nil != sl.config.ServiceTags && 1 == len(sl.config.ServiceTags) {
		tag = sl.config.ServiceTags[0]
	}

	// try to build watchplan
	sl.wp, err = WatchService(sl.config.ServiceName, tag, passingOnly, sl.config.AllowStale, sl.config.Token, sl.config.Datacenter)
	if err != nil {
		sl.mu.Unlock()
		return fmt.Errorf("unable to create watch plan: %v", err)
	}
	sl.wp.HybridHandler = sl.watchHandler

	go func() {
		//err := sl.wp.Run(sl.client.config.Address)
		//sl.mu.Lock()
		//if err != nil {
		//	sl.log.Printf("Watch Plan stopped with error: %s", err)
		//} else {
		//	sl.log.Print("Watch Plan stopped without error")
		//}
		//sl.wp = nil
		//sl.mu.Unlock()
	}()

	sl.mu.Unlock()

	return nil
}

// RemoveCallbacks will empty out the map of registered callbacks
func (sl *SiblingLocator) RemoveCallbacks() {
	sl.mu.Lock()
	sl.callbacks = make(map[string]SiblingCallback)
	sl.mu.Unlock()
}

// StopWatcher will stop the sibling watchplan.  If the plan was previously stopped, this is a noop.
func (sl *SiblingLocator) StopWatcher() {
	sl.mu.Lock()
	if sl.wp != nil && !sl.wp.IsStopped() {
		sl.wp.Stop()
		sl.wp = nil
	}
	sl.mu.Unlock()
}

// Current will immediately execute a Health().Service() call, returning and optionally sending the result to
// any registered callbacks
//
// - passingOnly will limit the response to only registrations deemed "healthy"
//
// - sendToCallbacks will send the results to any callbacks registered at time of execution.  Your callbacks can
// determine the difference between a watcher update and a "Current" call by looking for math.MaxUint64 as the index value
func (sl *SiblingLocator) Current(passingOnly, sendToCallbacks bool) (Siblings, error) {
	var tag string
	if nil != sl.config.ServiceTags && 1 == len(sl.config.ServiceTags) {
		tag = sl.config.ServiceTags[0]
	}

	svcs, _, err := sl.client.Health().Service(sl.config.ServiceName, tag, passingOnly, &api.QueryOptions{
		Datacenter: sl.config.Datacenter,
		Token:      sl.config.Token,
		AllowStale: sl.config.AllowStale,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to locate current siblings: %v", err)
	}

	if sendToCallbacks {
		sl.sendToCallbacks(math.MaxUint64, svcs)
	}

	return buildSiblingList(sl.config.NodeName, sl.config.ServiceID, sl.config.ServiceTags, svcs), nil
}

func (sl *SiblingLocator) watchHandler(val watch.BlockingParamVal, data interface{}) {
	bp, ok := val.(watch.WaitIndexVal)
	if !ok {
		sl.log.Printf("Watch Handler saw unexpected watch.WaitIndexVal type: %T", val)
		return
	}
	svcs, ok := data.([]*api.ServiceEntry)
	if !ok {
		sl.log.Printf("Watch Handler expected to see \"[]*api.ServiceEntry\", got \"%T\" instead...", data)
		return
	}

	sl.sendToCallbacks(uint64(bp), svcs)
}

func (sl *SiblingLocator) sendToCallbacks(index uint64, svcs []*api.ServiceEntry) {
	sl.mu.Lock()
	for _, receiver := range sl.callbacks {
		go receiver(index, buildSiblingList(sl.config.NodeName, sl.config.ServiceID, sl.config.ServiceTags, svcs))
	}
	sl.mu.Unlock()
}

func buildSiblingList(localNode, localID string, tags []string, svcs []*api.ServiceEntry) Siblings {
	siblings := make(Siblings, 0)

serviceLoop:
	for _, svc := range svcs {
		// omit myself
		if svc.Node.Node == localNode && svc.Service.ID == localID {
			continue serviceLoop
		}

	tagLoop:
		for _, t := range tags {
			for _, st := range svc.Service.Tags {
				if t == st {
					continue tagLoop
				}
			}
			continue serviceLoop
		}

		// add siblings
		siblings = append(siblings, buildSibling(svc))
	}

	return siblings
}

func buildSibling(svc *api.ServiceEntry) Sibling {
	return Sibling{
		*svc.Node,
		*svc.Service,
		svc.Checks,
	}
}
