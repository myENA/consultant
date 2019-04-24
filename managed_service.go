package consultant

import (
	"fmt"
	"github.com/hashicorp/consul/api"
	"github.com/myENA/consultant/log"
	"github.com/myENA/go-helpers"
	"sync"
)

// ManagedServiceRegister will return an instance of ManagedService after registering service
//
// NOTE: This forces the "EnableTagOverride" option to "true"
func (c *Client) ManagedServiceRegister(reg *SimpleServiceRegistration) (*ManagedService, error) {
	reg.EnableTagOverride = true

	sid, err := c.SimpleServiceRegister(reg)
	if err != nil {
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
	mu sync.RWMutex

	log    log.Logger
	client *Client

	meta           *ManagedServiceMeta
	candidate      *Candidate
	siblingLocator *SiblingLocator

	logSlug      string
	logSlugSlice []interface{}
}

func NewManagedService(client *Client, serviceID, serviceName string, registeredTags []string) (*ManagedService, error) {
	return &ManagedService{
		client: client,
		log:    log.New(serviceID),
		meta: &ManagedServiceMeta{
			id:                   serviceID,
			name:                 serviceName,
			registeredTags:       registeredTags,
			registeredTagsLength: len(registeredTags),
		},
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
	ms.mu.Lock()

	if nil != ms.candidate {
		ms.candidate.DeregisterUpdates()
		ms.candidate.Resign()
	}

	candidate, err := NewCandidate(ms.client, ms.client.MyAddr(), key, ttl)
	if err != nil {
		ms.mu.Unlock()
		return nil, err
	}

	if wait {
		candidate.Wait()
	}
	ms.candidate = candidate

	ms.mu.Unlock()

	return candidate, nil
}

// Candidate returns the current candidate for this service.  Does not create one
func (ms *ManagedService) Candidate() *Candidate {
	ms.mu.RLock()
	candidate := ms.candidate
	ms.mu.RUnlock()
	return candidate
}

// NewSiblingLocator will attempt to construct a SiblingLocator for this service.
//
// NOTE: If a SiblingLocator was previously created, it will be overwritten
func (ms *ManagedService) NewSiblingLocator(allowStale bool) (*SiblingLocator, error) {
	ms.mu.Lock()

	if nil != ms.siblingLocator {
		ms.siblingLocator.StopWatcher()
		ms.siblingLocator.RemoveCallbacks()
	}

	myNode, err := ms.client.Agent().NodeName()
	if err != nil {
		return nil, fmt.Errorf("unable to determine local Consul node name: %s", err)
	}

	siblingLocator, err := NewSiblingLocator(ms.client, SiblingLocatorConfig{
		ServiceID:   ms.meta.ID(),
		NodeName:    myNode,
		ServiceName: ms.meta.Name(),
		ServiceTags: ms.meta.RegisteredTags(),
		AllowStale:  allowStale,
		Datacenter:  ms.client.config.Datacenter,
		Token:       ms.client.config.Token,
	})

	if err != nil {
		ms.mu.Unlock()
		return nil, err
	}

	ms.siblingLocator = siblingLocator
	ms.mu.Unlock()

	return siblingLocator, nil
}

// SiblingLocator returns the current SiblingLocator for this service. Does not create one
func (ms *ManagedService) SiblingLocator() *SiblingLocator {
	ms.mu.RLock()
	siblingLocator := ms.siblingLocator
	ms.mu.RUnlock()
	return siblingLocator
}

// AddTags will attempt to add the provided tags to the service registration in consul
//
// - Input is "uniqued" before processing occurs.
// - If delta is 0, this is a no-op
func (ms *ManagedService) AddTags(tags ...string) error {
	ms.mu.Lock()

	// unique-ify it
	tags = helpers.UniqueStringSlice(tags)

	// if empty...
	if 0 == len(tags) {
		ms.mu.Unlock()
		return nil
	}

	serviceID := ms.meta.ID()
	serviceName := ms.meta.Name()

	// locate current definition as it exists within consul
	currentDefs, _, err := ms.client.Catalog().Service(serviceName, serviceID, nil)
	if err != nil {
		ms.mu.Unlock()
		return err
	}

	// if we couldn't, something bad has happened...
	if currentDefs == nil || len(currentDefs) == 0 {
		ms.mu.Unlock()
		return fmt.Errorf(
			"service \"%s\" with tag \"%s\" not found in Catalog",
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
		ms.log.Print("No new tags were found, will not execute watchers")
		ms.mu.Unlock()
		return nil
	}

	// log and try to update
	ms.log.Printf("\"%d\" new tags found, updating registration...", additions)
	err = ms.client.Agent().ServiceRegister(&api.AgentServiceRegistration{
		ID:                def.ServiceID,
		Name:              def.ServiceName,
		Address:           def.ServiceAddress,
		Port:              def.ServicePort,
		Tags:              newTags,
		EnableTagOverride: def.ServiceEnableTagOverride,
	})

	ms.mu.Unlock()

	return err
}

// RemoveTags will attempt to remove the provided set of tags from the service registration in consul.
//
// - You cannot remove the Service ID tag.
// - Input is "uniqued" before processing occurs.
// - If delta is 0, this is a no-op.
func (ms *ManagedService) RemoveTags(tags ...string) error {
	ms.mu.Lock()

	// unique-ify stuff
	tags = helpers.UniqueStringSlice(tags)

	// if empty...
	if 0 == len(tags) {
		ms.mu.Unlock()
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
		ms.mu.Unlock()
		return nil
	}

	// locate current definition as it exists in consul...
	currentDefs, _, err := ms.client.Catalog().Service(serviceName, serviceID, nil)
	if err != nil {
		ms.mu.Unlock()
		return err
	}

	// if we couldn't, something bad has happened...
	if currentDefs == nil || len(currentDefs) == 0 {
		ms.mu.Unlock()
		return fmt.Errorf(
			"current Service \"%s\" with tag \"%s\" not found in Catalog",
			serviceName,
			serviceID)
	}

	// should only be one
	// TODO: Complain if we find more than one?
	def := currentDefs[0]

	// build new tag slice
	newTags, removed := helpers.RemoveStringsFromSlice(def.ServiceTags, okt)
	if 0 == removed {
		ms.log.Printf("No tags were removed, will not execute watchers")
		ms.mu.Unlock()
		return nil
	}

	// log and try to update
	ms.log.Printf("\"%d\" tags were removed, updating registration...", removed)
	err = ms.client.Agent().ServiceRegister(&api.AgentServiceRegistration{
		ID:                def.ServiceID,
		Name:              def.ServiceName,
		Address:           def.ServiceAddress,
		Port:              def.ServicePort,
		Tags:              newTags,
		EnableTagOverride: def.ServiceEnableTagOverride,
	})

	ms.mu.Unlock()

	return err
}

// Deregister will remove this service from the service catalog in consul
func (ms *ManagedService) Deregister() error {
	ms.mu.RLock()

	// shut candidate down
	if nil != ms.candidate {
		ms.candidate.Resign()
	}

	// remove our service entry from consul
	err := ms.client.Agent().ServiceDeregister(ms.meta.ID())
	ms.mu.RUnlock()

	return err
}
