package consultant

import (
	"fmt"
	"github.com/hashicorp/consul/api"
	"github.com/myENA/go-helpers"
	"sync"
)

// ManagedServiceRegister will return an instance of ManagedService after registering service
//
// NOTE: This forces the "EnableTagOverride" option to "true"
func (c *Client) ManagedServiceRegister(reg *SimpleServiceRegistration) (*ManagedService, error) {
	reg.EnableTagOverride = true

	sid, err := c.SimpleServiceRegister(reg)
	if nil != err {
		return nil, err
	}

	return NewManagedService(c, sid, reg.Name, reg.Tags)
}

// ManagedServiceMeta is a small container object for on-creation details on the service
type ManagedServiceMeta struct {
	id   string
	name string

	registeredTags       []string
	registeredTagsLength int
}

func (m *ManagedServiceMeta) ID() string {
	return m.id
}

func (m *ManagedServiceMeta) Name() string {
	return m.name
}

// Returns list of tags service was registered with
func (m *ManagedServiceMeta) RegisteredTags() []string {
	tmp := make([]string, m.registeredTagsLength)
	copy(tmp, m.registeredTags)
	return tmp
}

// ManagedService is a service lifecycle helper object.  It provides an easy api to add / remove tags, create
// a SiblingWatcher or Candidate, and deregistration.
//
// NOTE: Currently no sanity checking is performed against Consul itself.  If you directly modify the service definition
// via the consul api / ui, this object will be defunct.
type ManagedService struct {
	client *Client

	lock sync.RWMutex

	meta *ManagedServiceMeta

	candidate      *Candidate
	siblingLocator *SiblingLocator

	logSlug      string
	logSlugSlice []interface{}
}

func NewManagedService(client *Client, serviceID, serviceName string, registeredTags []string) (*ManagedService, error) {
	return &ManagedService{
		client: client,
		meta: &ManagedServiceMeta{
			id:                   serviceID,
			name:                 serviceName,
			registeredTags:       registeredTags,
			registeredTagsLength: len(registeredTags),
		},
		logSlug:      fmt.Sprintf("[%s]", serviceID),
		logSlugSlice: []interface{}{fmt.Sprintf("[%s]", serviceID)},
	}, nil
}

// Meta returns service metadata object containing ID, Name, and the Tags that were present at registration time
func (ms *ManagedService) Meta() *ManagedServiceMeta {
	return ms.meta
}

// NewCandidate will attempt to construct a Candidate for this service
//
// NOTE: If a Candidate was previously created, it will be halted, removed, and a new one created
func (ms *ManagedService) NewCandidate(key, ttl string, wait bool) (*Candidate, error) {
	var err error

	if nil != ms.candidate {
		ms.candidate.DeregisterUpdates()
		ms.candidate.Resign()
	}

	ms.candidate, err = NewCandidate(ms.client, ms.client.MyAddr(), key, ttl)
	if nil != err {
		return nil, err
	}

	if wait {
		ms.candidate.Wait()
	}

	return ms.candidate, nil
}

// Candidate returns the current candidate for this service.  Does not create one
func (ms *ManagedService) Candidate() *Candidate {
	return ms.candidate
}

// NewSiblingLocator will attempt to construct a SiblingLocator for this service.
//
// NOTE: If a SiblingLocator was previously created, it will be overwritten
func (ms *ManagedService) NewSiblingLocator(allowStale bool) (*SiblingLocator, error) {
	var err error
	if nil != ms.siblingLocator {
		ms.siblingLocator.StopWatcher()
		ms.siblingLocator.RemoveCallbacks()
	}

	ms.siblingLocator, err = NewSiblingLocator(SiblingLocatorConfig{
		Client:      ms.client,
		ServiceID:   ms.meta.ID(),
		NodeName:    ms.client.MyNode(),
		ServiceName: ms.meta.Name(),
		ServiceTags: ms.meta.RegisteredTags(),
		AllowStale:  allowStale,
		Datacenter:  ms.client.config.Datacenter,
		Token:       ms.client.config.Token,
	})

	if nil != err {
		return nil, err
	}

	return ms.siblingLocator, nil
}

// SiblingLocator returns the current SiblingLocator for this service. Does not create one
func (ms *ManagedService) SiblingLocator() *SiblingLocator {
	return ms.siblingLocator
}

// AddTags will attempt to add the provided tags to the service registration in consul
//
// - Input is "uniqued" before processing occurs.
// - If delta is 0, this is a no-op
func (ms *ManagedService) AddTags(tags ...string) error {
	ms.lock.RLock()
	defer ms.lock.RUnlock()

	// unique-ify it
	tags = helpers.UniqueStringSlice(tags)

	// if empty...
	if 0 == len(tags) {
		return nil
	}

	serviceID := ms.meta.ID()
	serviceName := ms.meta.Name()

	// locate current definition as it exists within consul
	currentDefs, _, err := ms.client.Catalog().Service(serviceName, serviceID, nil)
	if nil != err {
		return err
	}

	// if we couldn't, something bad has happened...
	if currentDefs == nil || len(currentDefs) == 0 {
		return fmt.Errorf(
			"Unable to locate current Service definition for \"%s\" with tag \"%s\" in Catalog",
			serviceName,
			serviceID)
	}

	// should only be one
	// TODO: Complain if there is more than one?
	def := currentDefs[0]

	// Build new tag slice...
	newTags, additions := helpers.CombineStringSlices(def.ServiceTags, tags)

	// if none were added, log and return
	if 0 == additions {
		ms.logPrint("No new tags were found, will not execute update")
		return nil
	}

	// log and try to update
	ms.logPrintf("\"%d\" new tags found, updating registration...", additions)
	err = ms.client.Agent().ServiceRegister(&api.AgentServiceRegistration{
		ID:                def.ServiceID,
		Name:              def.ServiceName,
		Address:           def.ServiceAddress,
		Port:              def.ServicePort,
		Tags:              newTags,
		EnableTagOverride: def.ServiceEnableTagOverride,
	})

	// if update failed...
	if nil != err {
		return err
	}

	return nil
}

// RemoveTags will attempt to remove the provided set of tags from the service registration in consul.
//
// - You cannot remove the Service ID tag.
// - Input is "uniqued" before processing occurs.
// - If delta is 0, this is a no-op.
func (ms *ManagedService) RemoveTags(tags ...string) error {
	ms.lock.RLock()
	defer ms.lock.RUnlock()

	// unique-ify stuff
	tags = helpers.UniqueStringSlice(tags)

	// if empty...
	if 0 == len(tags) {
		return nil
	}

	serviceID := ms.meta.ID()
	serviceName := ms.meta.Name()

	// ensure we don't clear out our service ID tag...
	okt := make([]string, 0)
	for _, tag := range tags {
		if tag != serviceID {
			okt = append(okt, tag)
		}
	}

	// if empty...
	if 0 == len(okt) {
		return nil
	}

	// locate current definition as it exists in consul...
	currentDefs, _, err := ms.client.Catalog().Service(serviceName, serviceID, nil)
	if nil != err {
		return err
	}

	// if we couldn't, something bad has happened...
	if currentDefs == nil || len(currentDefs) == 0 {
		return fmt.Errorf(
			"Unable to locate current Service definition for \"%s\" with tag \"%s\" in Catalog",
			serviceName,
			serviceID)
	}

	// should only be one
	// TODO: Complain if we find more than one?
	def := currentDefs[0]

	// build new tag slice
	newTags, removed := helpers.RemoveStringsFromSlice(def.ServiceTags, okt)
	if 0 == removed {
		ms.logPrint("No tags were removed, will not execute update")
		return nil
	}

	// log and try to update
	ms.logPrintf("\"%d\" tags were removed, updating registration...", removed)
	err = ms.client.Agent().ServiceRegister(&api.AgentServiceRegistration{
		ID:                def.ServiceID,
		Name:              def.ServiceName,
		Address:           def.ServiceAddress,
		Port:              def.ServicePort,
		Tags:              newTags,
		EnableTagOverride: def.ServiceEnableTagOverride,
	})

	// if update failed...
	if nil != err {
		return err
	}

	return nil
}

// Deregister will remove this service from the service catalog in consul
func (ms *ManagedService) Deregister() error {
	ms.lock.Lock()
	defer ms.lock.Unlock()

	// remove our service entry from consul
	ms.client.Agent().ServiceDeregister(ms.meta.ID())

	// shut candidate down
	if nil != ms.candidate {
		ms.candidate.Resign()
	}

	ms.logPrint("Service has been deregistered from Consul")

	return nil
}

func (ms *ManagedService) logPrintf(format string, v ...interface{}) {
	log.Printf(fmt.Sprintf("%s %s", ms.logSlug, format), v...)
}

func (ms *ManagedService) logPrint(v ...interface{}) {
	log.Print(append(ms.logSlugSlice, v...)...)
}

func (ms *ManagedService) logPrintln(v ...interface{}) {
	log.Println(append(ms.logSlugSlice, v...)...)
}

func (ms *ManagedService) logFatalf(format string, v ...interface{}) {
	log.Fatalf(fmt.Sprintf("%s %s", ms.logSlug, format), v...)
}

func (ms *ManagedService) logFatal(v ...interface{}) {
	log.Fatal(append(ms.logSlugSlice, v...)...)
}

func (ms *ManagedService) logFatalln(v ...interface{}) {
	log.Fatalln(append(ms.logSlugSlice, v...)...)
}

func (ms *ManagedService) logPanicf(format string, v ...interface{}) {
	log.Panicf(fmt.Sprintf("%s %s", ms.logSlug, format), v...)
}

func (ms *ManagedService) logPanic(v ...interface{}) {
	log.Panic(append(ms.logSlugSlice, v...)...)
}

func (ms *ManagedService) logPanicln(v ...interface{}) {
	log.Panicln(append(ms.logSlugSlice, v...)...)
}
