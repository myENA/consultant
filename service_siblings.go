package consultant

import (
	"strconv"
	"strings"
	"sync"

	"github.com/hashicorp/consul/api"
)

// TODO: probably bad idea
type ServiceMeta struct {
	Node              string
	ID                string
	Name              string
	Port              int
	EnableTagOverride bool
	Tags              []string
}

// ServiceMetaFromCatalogService will construct a ServiceMeta struct from a consul api catalog service struct
func ServiceMetaFromCatalogService(cs *api.CatalogService) *ServiceMeta {
	m := &ServiceMeta{
		Node:              cs.Node,
		ID:                cs.ServiceID,
		Name:              cs.ServiceName,
		Port:              cs.ServicePort,
		EnableTagOverride: cs.ServiceEnableTagOverride,
	}

	if nil == cs.ServiceTags {
		m.Tags = make([]string, 0)
	} else {
		m.Tags = make([]string, len(cs.ServiceTags))
		copy(m.Tags, cs.ServiceTags)
	}

	return m
}

// ServiceMetaFromAgentService will construct a ServiceMeta struct from a consul api node and agent service struct
func ServiceMetaFromAgentService(n *api.Node, as *api.AgentService) *ServiceMeta {
	m := &ServiceMeta{
		Node:              n.Node,
		ID:                as.ID,
		Name:              as.Service,
		Port:              as.Port,
		EnableTagOverride: as.EnableTagOverride,
	}

	if nil == as.Tags {
		m.Tags = make([]string, 0)
	} else {
		m.Tags = make([]string, len(as.Tags))
		copy(m.Tags, as.Tags)
	}

	return m
}

type SiblingWatcherCallback func(index uint64, siblings map[string]*api.ServiceEntry)

type siblingCallbackContainer struct {
	receivers map[string]SiblingWatcherCallback
	lock      sync.RWMutex
	lazyName  uint64
}

func (rc *siblingCallbackContainer) add(name string, receiver SiblingWatcherCallback) string {
	rc.lock.Lock()
	defer rc.lock.Unlock()

	name = strings.TrimSpace(name)
	if "" == name {
		name = strconv.FormatUint(rc.lazyName, 10)
		rc.lazyName++
	}

	rc.receivers[name] = receiver

	return name
}

func (rc *siblingCallbackContainer) remove(name string) {
	rc.lock.Lock()
	defer rc.lock.Unlock()
	delete(rc.receivers, name)
}

func (rc *siblingCallbackContainer) send(omitID string, includeTags []string, index uint64, serviceList []*api.ServiceEntry) {
	rc.lock.RLock()
	defer rc.lock.RUnlock()

	if debug {
		log.Printf("[siblings] Sending \"%d\" services to \"%d\" receivers...", len(serviceList), len(rc.receivers))
	}

	for _, receiver := range rc.receivers {
		go receiver(index, makeServiceMap(omitID, includeTags, serviceList))
	}
}

type siblingWatcher struct {
	client *api.Client

	meta *ServiceMeta

	passingOnly bool

	plan    *WatchPlan
	handler WatchHandler
	lock    sync.Mutex
	running bool
}

//func newSiblingWatcher(c *api.Client, meta *ServiceMeta, passingOnly bool) (*siblingWatcher, error) {
//	var err error
//
//	sw := &siblingWatcher{
//		client:      c,
//		meta:        meta,
//		passingOnly: passingOnly,
//	}
//
//	return sw, nil
//}
//
//func (sw *siblingWatcher) start(serviceName, serviceTag string, serviceTags []string, passingOnly bool) error {
//	sw.lock.Lock()
//	defer sw.lock.Unlock()
//
//	if sw.running {
//		return getServiceSiblingError(ServiceSiblingErrorAlreadyRunning)
//	}
//
//	var err error
//
//	if "" != serviceTag {
//		sw.plan, err = NewServiceWatchPlan(sw.client, serviceName, serviceTag, passingOnly, sw.handler)
//	} else {
//
//	}
//
//	sw.plan, err = NewServiceWatchPlan(sw.client, serviceName, "", passingOnly, nil, sw.handler)
//	if nil != err {
//		return err
//	}
//
//	go sw.client.RunPlan(sw.plan)
//
//	sw.running = true
//	sw.passingOnly = passingOnly
//
//	log.Infof("[sibling-watcher] Watcher for service \"%s\" started.", sw.meta.Name())
//
//	return nil
//}
//
//func (sw *siblingWatcher) stop() {
//	sw.lock.Lock()
//	defer sw.lock.Unlock()
//
//	if sw.running {
//		sw.plan.Stop()
//		sw.running = false
//		sw.passingOnly = false
//
//		log.Infof("[sibling-watcher] Watcher for service \"%s\" stopped.", sw.meta.Name())
//	}
//}
//
//func (sw *siblingWatcher) watchingPassingOnly() bool {
//	return sw.passingOnly
//}
//
//type ServiceSiblings struct {
//	client *api.Client
//
//	serviceName string
//	serviceTags []string
//
//	callbacks *siblingCallbackContainer
//}
//
//func NewServiceSiblings(c *api.Client, serviceName string, serviceTags []string) (*ServiceSiblings, error) {
//	serviceName = strings.TrimSpace(serviceName)
//	if "" == serviceName {
//		return nil, getServiceSiblingError(ServiceSiblingErrorNameEmpty)
//	}
//
//	if 0 < len(serviceTags) {
//		serviceTags = gohelpers.UniqueStringSlice(serviceTags)
//	}
//
//	return &ServiceSiblings{}, nil
//}

func makeServiceMap(omitID string, includeTags []string, svcs []*api.ServiceEntry) map[string]*api.ServiceEntry {
	serviceMap := make(map[string]*api.ServiceEntry)

ServiceLoop:
	for _, svc := range svcs {
		// omit myself
		if svc.Service.ID == omitID {
			continue ServiceLoop
		}

	TagLoop:
		for _, t := range includeTags {
			if t == omitID {
				continue TagLoop
			}
			for _, st := range svc.Service.Tags {
				if t == st {
					continue TagLoop
				}
			}
			continue ServiceLoop
		}

		// add siblings
		serviceMap[svc.Service.ID] = svc
	}

	return serviceMap
}
