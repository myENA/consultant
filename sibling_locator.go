package consultant

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/watch"
)

// SiblingCallback is the prototype for a callback that can be registered in a SiblingLocator.  It will be called
// whenever a running watcher receives a new service list from Consul, or optionally when a call to Current is made.
//
// "index" will be "math.MaxUint64" when called using "Current"
type SiblingCallback func(index uint64, siblings Siblings)

// Sibling is a thread-safe representation of the Consul api.ServiceEntry object returned by the Health().Service() api
type Sibling struct {
	Node    api.Node
	Service api.AgentService
	Checks  []api.HealthCheck
}

// Siblings is provided to any callbacks
type Siblings []Sibling

// SiblingLocatorConfig is used to construct a SiblingLocator.  All values except ServiceTags are required.
type SiblingLocatorConfig struct {
	Client         *api.Client // REQUIRED consul api client
	LocalServiceID string      // REQUIRED ID of local service you want to find siblings for.  Used to exclude local service from responses
	LocalNodeName  string      // REQUIRED name of local node where service was registered.  Used to exclude local service from responses
	ServiceName    string      // REQUIRED name of service
	ServiceTags    []string    // OPTIONAL tags to require when looking for siblings
}

// SiblingLocator provides a way for a local service to find other services registered in Consul that share it's name
// and tags (if any).
type SiblingLocator struct {
	config *SiblingLocatorConfig

	callbacks        map[string]SiblingCallback
	callbacksLock    sync.RWMutex
	lazyCallbackName uint64

	wp        *watch.WatchPlan
	wpLock    sync.Mutex
	wpRunning bool

	logSlug      string
	logSlugSlice []interface{}
}

func NewSiblingLocator(config SiblingLocatorConfig) (*SiblingLocator, error) {
	sl := &SiblingLocator{
		config:    &config,
		callbacks: make(map[string]SiblingCallback),
	}

	sl.config.LocalServiceID = strings.TrimSpace(sl.config.LocalServiceID)
	if "" == sl.config.LocalServiceID {
		return nil, getSiblingLocatorError(SiblingLocatorErrorLocalIDEmpty)
	}

	sl.config.ServiceName = strings.TrimSpace(sl.config.ServiceName)
	if "" == sl.config.ServiceName {
		return nil, getSiblingLocatorError(SiblingLocatorErrorNameEmpty)
	}

	if nil == sl.config.ServiceTags || 0 == len(sl.config.ServiceTags) {
		sl.config.ServiceTags = make([]string, 0)
	} else {
		tmp := make([]string, len(sl.config.ServiceTags))
		copy(tmp, sl.config.ServiceTags)
		sl.config.ServiceTags = tmp
	}

	sl.logSlug = fmt.Sprintf("[sibling-locator-%s]", sl.config.ServiceName)
	sl.logSlugSlice = []interface{}{sl.logSlug}

	return sl, nil
}

// NewSiblingLocatorWithCatalogService will construct a SiblingLocator from a consul api catalog service struct
func NewSiblingLocatorWithCatalogService(c *api.Client, cs *api.CatalogService) (*SiblingLocator, error) {
	conf := &SiblingLocatorConfig{
		Client:         c,
		LocalNodeName:  cs.Node,
		LocalServiceID: cs.ServiceID,
		ServiceName:    cs.ServiceName,
	}

	if nil == cs.ServiceTags {
		conf.ServiceTags = make([]string, 0)
	} else {
		conf.ServiceTags = make([]string, len(cs.ServiceTags))
		copy(conf.ServiceTags, cs.ServiceTags)
	}

	return NewSiblingLocator(*conf)
}

// NewSiblingLocatorWithAgentService will construct a SiblingLocator from a consul api node and agent service struct
func NewSiblingLocatorWithAgentService(c *api.Client, n *api.Node, as *api.AgentService) (*SiblingLocator, error) {
	conf := &SiblingLocatorConfig{
		Client:         c,
		LocalNodeName:  n.Node,
		LocalServiceID: as.ID,
		ServiceName:    as.Service,
	}

	if nil == as.Tags {
		conf.ServiceTags = make([]string, 0)
	} else {
		conf.ServiceTags = make([]string, len(as.Tags))
		copy(conf.ServiceTags, as.Tags)
	}

	return NewSiblingLocator(*conf)
}

func (sl *SiblingLocator) AddCallback(name string, cb SiblingCallback) string {
	sl.callbacksLock.Lock()
	defer sl.callbacksLock.Unlock()

	name = strings.TrimSpace(name)
	if "" == name {
		name = strconv.FormatUint(sl.lazyCallbackName, 10)
		sl.lazyCallbackName++
	}

	sl.callbacks[name] = cb

	return name
}

func (sl *SiblingLocator) RemoveCallback(name string) {
	sl.callbacksLock.Lock()
	defer sl.callbacksLock.Unlock()
	delete(sl.callbacks, name)
}

func (sl *SiblingLocator) StartWatcher(passingOnly bool) error {
	sl.wpLock.Lock()
	defer sl.wpLock.Unlock()

	if sl.wpRunning {
		return getSiblingLocatorError(SiblingLocatorErrorWatcherAlreadyRunning)
	}

	var err error
	var tag string

	if nil != sl.config.ServiceTags && 1 == len(sl.config.ServiceTags) {
		tag = sl.config.ServiceTags[0]
	}

	params := map[string]interface{}{
		"type":        "service",
		"stale":       false,
		"service":     sl.config.ServiceName,
		"tag":         tag,
		"passingonly": passingOnly,
	}

	sl.wp, err = watch.Parse(params)
	if nil != err {
		sl.logPrintf("Unable to create watch plan: %v", err)
		return getSiblingLocatorError(SiblingLocatorErrorWatcherCreateFailed)
	}

	sl.wp.Handler = sl.watchHandler

	go func() {
		err := sl.wp.Run("")
		if nil != err {
			sl.wpLock.Lock()
			sl.logPrintf("WatchPlan stopped with error: %v", err)
			sl.wpRunning = false
			sl.wp = nil
			sl.wpLock.Unlock()
		}
	}()

	return nil
}

func (sl *SiblingLocator) StopWatcher() {
	sl.wpLock.Lock()
	defer sl.wpLock.Unlock()

	if nil != sl.wp {
		sl.wp.Stop()
		sl.wpRunning = false
		sl.wp = nil
	}
}

func (sl *SiblingLocator) Current(passingOnly, sendToCallbacks bool, options *api.QueryOptions) (Siblings, error) {
	var tag string
	if nil != sl.config.ServiceTags && 1 == len(sl.config.ServiceTags) {
		tag = sl.config.ServiceTags[0]
	}

	svcs, _, err := sl.config.Client.Health().Service(sl.config.ServiceName, tag, passingOnly, options)
	if nil != err {
		sl.logPrintf("Unable to locate current siblings: %v", err)
		return nil, getSiblingLocatorError(SiblingLocatorErrorCurrentCallFailed)
	}

	if sendToCallbacks {
		sl.sendToCallbacks(math.MaxUint64, svcs)
	}

	return buildSiblingList(sl.config.LocalNodeName, sl.config.LocalServiceID, sl.config.ServiceTags, svcs), nil
}

func (sl *SiblingLocator) watchHandler(index uint64, data interface{}) {
	svcs, ok := data.([]*api.ServiceEntry)
	if !ok {
		sl.logPrintf("Watch Handler expected to see \"[]*api.ServiceEntry\", got "+
			"\"%s\" instead...",
			reflect.TypeOf(data).Kind().String())
		return
	}

	sl.sendToCallbacks(index, svcs)
}

func (sl *SiblingLocator) sendToCallbacks(index uint64, svcs []*api.ServiceEntry) {
	sl.callbacksLock.RLock()
	defer sl.callbacksLock.RUnlock()

	for _, receiver := range sl.callbacks {
		go receiver(index, buildSiblingList(sl.config.LocalNodeName, sl.config.LocalServiceID, sl.config.ServiceTags, svcs))
	}
}

func (sl *SiblingLocator) logPrintf(format string, v ...interface{}) {
	log.Printf(fmt.Sprintf("%s %s", sl.logSlug, format), v...)
}

func (sl *SiblingLocator) logPrint(v ...interface{}) {
	log.Print(append(sl.logSlugSlice, v...)...)
}

func (sl *SiblingLocator) logPrintln(v ...interface{}) {
	log.Println(append(sl.logSlugSlice, v...)...)
}

func (sl *SiblingLocator) logFatalf(format string, v ...interface{}) {
	log.Fatalf(fmt.Sprintf("%s %s", sl.logSlug, format), v...)
}

func (sl *SiblingLocator) logFatal(v ...interface{}) {
	log.Fatal(append(sl.logSlugSlice, v...)...)
}

func (sl *SiblingLocator) logFatalln(v ...interface{}) {
	log.Fatalln(append(sl.logSlugSlice, v...)...)
}

func (sl *SiblingLocator) logPanicf(format string, v ...interface{}) {
	log.Panicf(fmt.Sprintf("%s %s", sl.logSlug, format), v...)
}

func (sl *SiblingLocator) logPanic(v ...interface{}) {
	log.Panic(append(sl.logSlugSlice, v...)...)
}

func (sl *SiblingLocator) logPanicln(v ...interface{}) {
	log.Panicln(append(sl.logSlugSlice, v...)...)
}

func buildSiblingList(localNode, localID string, tags []string, svcs []*api.ServiceEntry) Siblings {
	siblings := make(Siblings, 0)

ServiceLoop:
	for _, svc := range svcs {
		// omit myself
		if svc.Node.Node == localNode && svc.Service.ID == localID {
			continue ServiceLoop
		}

	TagLoop:
		for _, t := range tags {
			for _, st := range svc.Service.Tags {
				if t == st {
					continue TagLoop
				}
			}
			continue ServiceLoop
		}

		// add siblings
		siblings = append(siblings, buildSibling(svc))
	}

	return siblings
}

func buildSibling(svc *api.ServiceEntry) Sibling {
	node := *svc.Node
	service := *svc.Service

	tmp := make(map[string]string)
	for k, v := range node.TaggedAddresses {
		tmp[k] = v
	}
	node.TaggedAddresses = tmp

	tmp = make(map[string]string)
	for k, v := range node.Meta {
		tmp[k] = v
	}
	node.Meta = tmp

	tmp1 := make([]string, len(service.Tags))
	copy(tmp1, service.Tags)
	service.Tags = tmp1

	checks := make([]api.HealthCheck, len(svc.Checks))
	for i, c := range svc.Checks {
		checks[i] = *c
	}

	return Sibling{
		Node:    node,
		Service: service,
		Checks:  checks,
	}
}
