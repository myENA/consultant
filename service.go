package consultant

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/api/watch"
	"github.com/myENA/go-helpers"
)

type ManagedServiceState uint8

const (
	ManagedServiceStateStopped    ManagedServiceState = 0x20
	ManagedServiceStateRunning    ManagedServiceState = 0x21
	ManagedServiceStateShutdowned ManagedServiceState = 0x22
)

func (s ManagedServiceState) String() string {
	switch s {
	case ManagedServiceStateRunning:
		return "running"
	case ManagedServiceStateStopped:
		return "stopped"
	case ManagedServiceStateShutdowned:
		return "shutdowned"

	default:
		return "UNKNOWN"
	}
}

const (
	ServiceDefaultIDFormat        = SlugName + "-" + SlugAddr + "-" + SlugRand
	ServiceDefaultRefreshInterval = api.ReadableDuration(30 * time.Second)
)

// ManagedServiceUpdate is the value of .Data in all Notification pushes from a ManagedService
type ManagedServiceUpdate struct {
	ServiceID     string    `json:"service_id"`
	ServiceName   string    `json:"service_name"`
	LastRefreshed time.Time `json:"last_refreshed"`
	Error         error     `json:"error"`
}

// ManagedServiceConfig describes the basis for a new ManagedService instance
type ManagedServiceConfig struct {
	// ID [required]
	//
	// ID of service to fetch to turn into a managed service
	ID string

	// BaseChecks [optional] (recommended)
	//
	// These are the base service checks that will be re-registered with the service should it be removed externally
	// from the node the service was registered to.
	//
	// This will most likely be removed in the future once https://github.com/hashicorp/consul/issues/1680 is finally
	// implemented, but for now is necessary if you wish for your service to automatically have its health checks
	// re-registered
	BaseChecks api.AgentServiceChecks

	// RefreshInterval [optional]
	//
	// Optionally specify a refresh renewInterval.  Defaults to value of ServiceDefaultRefreshInterval.
	RefreshInterval api.ReadableDuration

	// QueryOptions [optional]
	//
	// Options to use whenever making a read api query.  This will be shallow copied per internal request made.
	QueryOptions *api.QueryOptions

	// WriteOptions [optional]
	//
	// Options to use whenever making a write api query.  This will be shallow copied per internal request made.
	WriteOptions *api.WriteOptions

	// RequestTTL [optional]
	//
	// Optionally specify a TTL to pass to internal API requests.  Defaults to 2 seconds
	RequestTTL time.Duration

	// Logger [optional]
	//
	// Optionally specify a logger.  No logging will take place if one is not provided
	Logger Logger

	// Debug [optional]
	//
	// If true, will enable debug-level logging if a logger is provided
	Debug bool

	// Client [optional]
	//
	// Optionally provide a Consul client instance to use.  If one is not defined, a new one will be created with
	// default configuration values.
	Client *api.Client
}

// ManagedService
//
// This type is a wrapper around an existing Consul agent service.  It provides several wrappers to make managing the
// lifecycle of a processes' service registration easier.
type ManagedService struct {
	*notifierBase
	mu sync.RWMutex

	state ManagedServiceState

	serviceID  string
	baseChecks api.AgentServiceChecks

	// svc MUST ALWAYS BE DEFINED.  If it cannot be fetched on boot, the service must die.
	//
	// it is updated by, and only by, the .refreshService() method, either when forced or via the normal maintenance
	// loop per refreshInterval value.  it is used to re-register the service should it be nuked outside of this
	// process, and as a basis for .AddTags() and .RemoveTags().
	svc *api.AgentService

	refreshInterval time.Duration
	localRefreshed  time.Time
	forceRefresh    chan chan error

	client *api.Client
	qo     *api.QueryOptions
	wo     *api.WriteOptions
	rttl   time.Duration

	stop chan chan error

	dbg    bool
	logger Logger
}

// NewManagedService creates a new ManagedService instance.
func NewManagedService(cfg *ManagedServiceConfig) (*ManagedService, error) {
	var (
		err error

		ms = new(ManagedService)
	)

	// maybe logger
	ms.dbg = cfg.Debug
	ms.logger = cfg.Logger

	ms.notifierBase = newNotifierBase(ms.logger, ms.dbg)

	if cfg == nil {
		return nil, errors.New("cfg cannot be nil")
	}
	if cfg.ID == "" {
		return nil, errors.New("id must be set in config")
	}

	// copy base checks to new slice.
	if l := len(cfg.BaseChecks); l > 0 {
		ms.baseChecks = make(api.AgentServiceChecks, l, l)
		copy(ms.baseChecks, cfg.BaseChecks)
	} else {
		ms.baseChecks = make(api.AgentServiceChecks, 0, 0)
	}

	// store service id
	ms.serviceID = cfg.ID

	// ensure we have a consul client
	if cfg.Client != nil {
		ms.client = cfg.Client
	} else if ms.client, err = api.NewClient(api.DefaultConfig()); err != nil {
		return nil, fmt.Errorf("error creating client with default config: %s", err)
	}

	// clone query options, if provided
	if cfg.QueryOptions != nil {
		ms.qo = new(api.QueryOptions)
		*ms.qo = *cfg.QueryOptions
	}

	// clone write options, if provided
	if cfg.WriteOptions != nil {
		ms.wo = new(api.WriteOptions)
		*ms.wo = *cfg.WriteOptions
	}

	// set refresh renewInterval
	if cfg.RefreshInterval != 0 {
		ms.refreshInterval = cfg.RefreshInterval.Duration()
	} else {
		ms.refreshInterval = time.Duration(ServiceDefaultRefreshInterval)
	}

	// misc
	if cfg.RequestTTL > 0 {
		ms.rttl = cfg.RequestTTL
	} else {
		ms.rttl = defaultInternalRequestTTL
	}
	ms.forceRefresh = make(chan chan error)
	ms.stop = make(chan chan error)

	// fetch initial service state from node
	ctx, cancel := context.WithTimeout(context.Background(), ms.rttl)
	defer cancel()
	if _, err = ms.refreshService(ctx); err != nil {
		return nil, fmt.Errorf("error fetching current state of service: %s", err)
	}

	return ms, ms.Register()
}

// State returns the current state of this managed service
func (ms *ManagedService) State() ManagedServiceState {
	ms.mu.RLock()
	s := ms.state
	ms.mu.RUnlock()
	return s
}

// Running returns true if state is "running"
func (ms *ManagedService) Running() bool {
	return ms.State() == ManagedServiceStateRunning
}

// Stopped returns true if state is "stopped"
func (ms *ManagedService) Stopped() bool {
	return ms.State() == ManagedServiceStateStopped
}

// Shutdowned returns true if state is "stopped"
func (ms *ManagedService) Shutdowned() bool {
	return ms.State() == ManagedServiceStateShutdowned
}

// LastRefreshed is the last time the internal state of the managed service was last updated
func (ms *ManagedService) LastRefreshed() time.Time {
	ms.mu.RLock()
	lr := ms.localRefreshed
	ms.mu.RUnlock()
	return lr
}

// ServiceID returns the ID of the service being managed
func (ms *ManagedService) ServiceID() string {
	if !ms.Running() {
		return ""
	}
	ms.mu.RLock()
	id := ms.svc.ID
	ms.mu.RUnlock()
	return id
}

// ServiceName returns the name of the service being managed
func (ms *ManagedService) ServiceName() string {
	if !ms.Running() {
		return ""
	}
	ms.mu.RLock()
	name := ms.svc.Service
	ms.mu.RUnlock()
	return name
}

// Register can be used to re-start this managed service instance if it was previously deregistered
func (ms *ManagedService) Register() error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if ms.state == ManagedServiceStateRunning {
		ms.logf(true, "Register() called but we're already running")
		return nil
	}

	if ms.state == ManagedServiceStateShutdowned {
		ms.logf(false, "Register() called but we're shutdowned")
		return errors.New("managed service is shutdowned")
	}

	ms.setState(ManagedServiceStateRunning)

	go ms.maintain()

	return nil
}

// Deregister stops internal maintenance routines and removes the managed service from the connected consul agent
func (ms *ManagedService) Deregister() error {
	ms.mu.Lock()

	if ms.state == ManagedServiceStateStopped {
		ms.mu.Unlock()
		ms.logf(true, "Deregister() called but we're already deregistered")
		return nil
	}

	if ms.state == ManagedServiceStateShutdowned {
		ms.mu.Unlock()
		ms.logf(false, "Deregister() called but we're shutdown")
		return errors.New("managed service is shutdowned")
	}

	ms.setState(ManagedServiceStateStopped)

	ms.mu.Unlock()

	return ms.waitForStop()
}

// Shutdown stops internal maintenance routines and removes the managed service from the connected consul agent.  Once
// shutdown, the managed service is considered defunct.
func (ms *ManagedService) Shutdown() error {
	ms.mu.Lock()
	if ms.state == ManagedServiceStateShutdowned {
		ms.mu.Unlock()
		ms.logf(true, "Shutdown() called but we're already shutdown")
		return nil
	}

	var (
		err error

		// do we need to perform stop action(s)?
		requiresStop = ms.state == ManagedServiceStateRunning
	)

	// set state and notify recipients
	ms.setState(ManagedServiceStateShutdowned)

	ms.mu.Unlock()

	// if stopping is required, do so.
	if requiresStop {
		err = ms.waitForStop()
	}

	// remove all notification recipients
	ms.DetachAllNotificationRecipients(true)

	// close chans
	ms.mu.Lock()
	close(ms.forceRefresh)
	close(ms.stop)
	ms.mu.Unlock()

	return err
}

// AgentService attempts to fetch the current AgentService entry for this ManagedService using Agent().Service()
func (ms *ManagedService) AgentService(ctx context.Context) (*api.AgentService, *api.QueryMeta, error) {
	if !ms.Running() {
		return nil, nil, errors.New("managed service is not running")
	}
	return ms.findAgentService(ctx)
}

// ServiceEntry attempts to fetch the current ServiceEntry entry for this ManagedService using Health().Service()
func (ms *ManagedService) ServiceEntry(ctx context.Context) (*api.ServiceEntry, *api.QueryMeta, error) {
	if !ms.Running() {
		return nil, nil, errors.New("managed service is not running")
	}
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return ms.findHealth(ctx)
}

// CatalogService attempts to fetch the current CatalogService entry for this ManagedService using Catalog().Service()
func (ms *ManagedService) CatalogService(ctx context.Context) (*api.CatalogService, *api.QueryMeta, error) {
	if !ms.Running() {
		return nil, nil, errors.New("managed service is not running")
	}
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return ms.findCatalog(ctx)
}

// Checks returns a list of all the checks registered to this specific service
func (ms *ManagedService) Checks(ctx context.Context) (api.HealthChecks, *api.QueryMeta, error) {
	if !ms.Running() {
		return nil, nil, errors.New("managed service is not running")
	}
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return ms.fetchChecks(ctx)
}

// AddTags attempts to add one or more tags to the service registration in consul, if and only if EnableTagOverride was
// enabled when the service was registered.
//
// Returns:
//	- count of tags added
//	- error, if unsuccessful
//
// If no tags were provided or the provided list of tags are already present on the service a 0, nil will be returned.
func (ms *ManagedService) AddTags(tags ...string) (int, error) {
	if !ms.Running() {
		return 0, errors.New("managed service is not running")
	}

	ms.mu.RLock()

	if !ms.svc.EnableTagOverride {
		ms.mu.RUnlock()
		return 0, errors.New("cannot add tags: EnableTagOverride was false at service registration")
	}

	ms.logf(true, "AddTags() - Adding tags %v to service %q...", tags, ms.serviceID)
	if len(tags) == 0 {
		ms.logf(true, "AddTags() - Empty tag set provided")
		ms.mu.RUnlock()
		return 0, nil
	}

	var (
		newTags []string
		added   int
		err     error
	)
	if newTags, added = helpers.CombineStringSlices(ms.svc.Tags, tags); added == 0 {
		ms.logf(true, "AddTags() - no new tags provided")
	} else if err = ms.registerService(false, newTags); err != nil {
		added = 0
	}

	ms.pushNotification(NotificationEventManagedServiceTagsAdded, ms.buildUpdate(err))

	ms.mu.RUnlock()

	if err == nil {
		err = ms.ForceRefresh()
	}

	return added, err
}

// RemoveTags attempts to remove one or more tags from the service registration in consul, if and only if
// EnableTagOverride was enabled when the service was registered.
//
// Returns:
//	- count of tags removed
// 	- error, if unsuccessful
//
// If no tags were provided or none of the tags in the list were present on the service, a 0, nil will be returned.
func (ms *ManagedService) RemoveTags(tags ...string) (int, error) {
	if !ms.Running() {
		return 0, errors.New("managed service is not running")
	}

	ms.mu.RLock()

	if !ms.svc.EnableTagOverride {
		ms.mu.RUnlock()
		return 0, errors.New("cannot remove tags: EnableTagOverride was false at service registration")
	}
	ms.logf(true, "RemoveTags() - Removing tags %v from service %q...", tags, ms.serviceID)

	if len(tags) == 0 {
		ms.logf(true, "RemoveTags() - Empty tag set provided")
		ms.mu.RUnlock()
		return 0, nil
	}

	var (
		newTags []string
		removed int
		err     error
	)

	if newTags, removed = helpers.RemoveStringsFromSlice(ms.svc.Tags, tags); removed == 0 {
		ms.logf(true, "RemoveTags() - Service does not have any tags from input %v", tags)
	} else if err = ms.registerService(false, newTags); err != nil {
		removed = 0
	}

	ms.pushNotification(NotificationEventManagedServiceTagsRemoved, ms.buildUpdate(err))

	ms.mu.RUnlock()

	if err == nil {
		err = ms.ForceRefresh()
	}

	return removed, err
}

// ForceRefresh attempts an immediate internal state refresh, blocking until attempt has been completed.
func (ms *ManagedService) ForceRefresh() error {
	if !ms.Running() {
		return errors.New("managed service is not running")
	}
	ms.logf(true, "ForceRefresh() - request received...")
	ch := make(chan error, 1)
	defer close(ch)
	ms.forceRefresh <- ch
	return <-ch
}

// buildUpdate constructs a notification update type
//
// caller must hold lock
func (ms *ManagedService) buildUpdate(err error) ManagedServiceUpdate {
	return ManagedServiceUpdate{
		ServiceID:     ms.svc.ID,
		ServiceName:   ms.svc.Service,
		LastRefreshed: ms.localRefreshed,
		Error:         err,
	}
}

// pushNotification constructs and then pushes a new notification to currently registered recipients based on the
// current state of the service.
func (ms *ManagedService) pushNotification(ev NotificationEvent, up ManagedServiceUpdate) {
	ms.sendNotification(NotificationSourceManagedService, ev, up)
}

// setState updates the internal state value and pushes a notification of change
//
// caller must hold full lock
func (ms *ManagedService) setState(state ManagedServiceState) {
	var ev NotificationEvent

	if ms.state == state {
		return
	}

	switch state {
	case ManagedServiceStateRunning:
		ev = NotificationEventManagedServiceRunning
	case ManagedServiceStateStopped:
		ev = NotificationEventManagedServiceStopped
	case ManagedServiceStateShutdowned:
		ev = NotificationEventManagedServiceShutdowned

	default:
		panic(fmt.Sprintf("unkonwn state %d (%[1]s) seen", state))
	}

	ms.state = state

	ms.pushNotification(ev, ms.buildUpdate(nil))
}

func (ms *ManagedService) logf(debug bool, f string, v ...interface{}) {
	if ms.logger == nil || debug && !ms.dbg {
		return
	}
	ms.logger.Printf(f, v...)
}

func (ms *ManagedService) waitForStop() error {
	drop := make(chan error, 1)
	ms.stop <- drop
	err := <-drop
	close(drop)
	return err
}

func (ms *ManagedService) findAgentService(ctx context.Context) (*api.AgentService, *api.QueryMeta, error) {
	return ms.client.Agent().Service(ms.serviceID, ms.qo.WithContext(ctx))
}

// findHealth must be used behind a lock
func (ms *ManagedService) findHealth(ctx context.Context) (*api.ServiceEntry, *api.QueryMeta, error) {
	var (
		svcs []*api.ServiceEntry
		qm   *api.QueryMeta
		err  error
	)
	if svcs, qm, err = ms.client.Health().ServiceMultipleTags(ms.svc.Service, nil, false, ms.qo.WithContext(ctx)); err != nil {
		return nil, qm, fmt.Errorf("error fetching service health: %s", err)
	}
	if svc, ok := SpecificServiceEntry(ms.serviceID, svcs); ok {
		return svc, qm, nil
	}
	return nil, qm, fmt.Errorf("no service with name %q with id %q found", ms.svc.Service, ms.svc.ID)
}

// findCatalog must be used behind a lock
func (ms *ManagedService) findCatalog(ctx context.Context) (*api.CatalogService, *api.QueryMeta, error) {
	var (
		svcs []*api.CatalogService
		qm   *api.QueryMeta
		err  error
	)
	if svcs, qm, err = ms.client.Catalog().ServiceMultipleTags(ms.svc.Service, nil, ms.qo.WithContext(ctx)); err != nil {
		return nil, nil, fmt.Errorf("error fetching service from catalog: %s", err)
	}
	if svc, ok := SpecificCatalogService(ms.serviceID, svcs); ok {
		return svc, qm, nil
	}
	return nil, qm, fmt.Errorf("no service with name %q with id %q found", ms.svc.Service, ms.serviceID)
}

// fetchChecks must be used behind a lock
func (ms *ManagedService) fetchChecks(ctx context.Context) (api.HealthChecks, *api.QueryMeta, error) {
	var (
		checks api.HealthChecks
		qm     *api.QueryMeta
		err    error
	)
	if checks, qm, err = ms.client.Health().Checks(ms.svc.Service, ms.qo.WithContext(ctx)); err != nil {
		return nil, qm, nil
	}
	return SpecificChecks(ms.serviceID, checks), qm, nil
}

// refreshService will refresh the internal representation of the service.  If fetch fails it will attempt to
// re-register the service based on the last-known definition, returning an error if that itself fails.
//
// caller must hold full lock
func (ms *ManagedService) refreshService(ctx context.Context) (*api.QueryMeta, error) {
	var (
		svc *api.AgentService
		qm  *api.QueryMeta
		err error
	)

	ms.logf(true, "refreshService() - Refreshing local service...")

	if svc, qm, err = ms.findAgentService(ctx); err != nil {
		if IsNotFoundError(err) {
			ms.logf(false, "refreshService() - Received 404 not found, attempting to re-register...")

			ms.pushNotification(NotificationEventManagedServiceMissing, ms.buildUpdate(err))

			if err = ms.registerService(true, ms.svc.Tags); err != nil {
				ms.logf(false, "refreshService() - Failed to re-register service: %s")
			} else if svc, qm, err = ms.findAgentService(ctx); err != nil {
				ms.logf(false, "refreshService() - Failed to locate re-registered service: %s", err)
			} else {
				ms.logf(false, "refreshService() - Service successfully re-registered")
			}

		} else {
			ms.logf(false, "refreshService() - Error fetching service from node: %s", err)
		}
	}

	if svc == nil && err == nil {
		err = errors.New("unable to locate service after re-register")
	}

	if err == nil {
		ms.svc = svc
		ms.localRefreshed = time.Now()

		ms.logf(true, "refreshService() - Service refreshed: %v", svc)
	}

	ms.pushNotification(NotificationEventManagedServiceRefreshed, ms.buildUpdate(err))

	return qm, err
}

// registerService will attempt to re-push the service to the consul agent
//
// caller must hold lock
func (ms *ManagedService) registerService(missing bool, tags []string) error {
	var err error
	ms.logf(false, "registerService() - Registering service with node...")

	reg := new(api.AgentServiceRegistration)
	reg.ID = ms.svc.ID
	reg.Name = ms.svc.Service
	reg.Tags = tags
	reg.Port = ms.svc.Port
	reg.Address = ms.svc.Address
	// always set EnableTagOverride to true
	reg.EnableTagOverride = true

	if missing {
		ms.logf(false, "registerService() - Upstream service is gone, redefining full service...")
		reg.Kind = ms.svc.Kind
		reg.TaggedAddresses = ms.svc.TaggedAddresses
		reg.Meta = ms.svc.Meta
		reg.Weights = &ms.svc.Weights
		reg.Proxy = ms.svc.Proxy
		reg.Connect = ms.svc.Connect

		// TODO: once possible, build checks based off current upstream definition
		reg.Checks = ms.baseChecks
	}

	if err = ms.client.Agent().ServiceRegister(reg); err != nil {
		ms.logf(false, "registerService() - Error registering service: %s", err)
	}

	return err
}

// buildWatchPlan constructs a new watch plan with appropriate handler defined
func (ms *ManagedService) buildWatchPlan(up chan<- watch.WaitIndexVal) (*watch.Plan, error) {
	var (
		token, datacenter string
		mu                sync.Mutex
		last              watch.WaitIndexVal
	)

	// if a query options instance was passed in at construction, extract token and dc values for use in watch plan
	if ms.qo != nil {
		token = ms.qo.Token
		datacenter = ms.qo.Datacenter
	}

	// build plan
	wp, err := WatchServiceMultipleTags(ms.svc.Service, ms.svc.Tags, false, true, token, datacenter)
	if err != nil {
		return nil, err
	}

	// this handler does not directly update the internal state, rather it watches for diffs in the provided "val"
	// variable.  if different from last the new index is pushed onto the "up" chan, which in turn is being read from
	// by the primary maintenance loop below.
	//
	// the primary maintenance loop then executes a refresh of the internal service state using the typical mechanism.
	//
	// this is to reduce the number of places we are performing the same work, and it also resets the maintenance timer
	// to help prevent noise.
	wp.HybridHandler = func(val watch.BlockingParamVal, _ interface{}) {
		var (
			bp watch.WaitIndexVal
			ok bool
		)

		// ensure we got expected type of param
		if bp, ok = val.(watch.WaitIndexVal); !ok {
			ms.logf(false, "Watcher expected val to be of type watch.WaitIndexVal, saw %T", val)
			return
		}

		// it is entirely possible we'll get an update after the service has been stopped or shutdowned.  there is
		// nothing to do with these messages.
		if !ms.Running() {
			ms.logf(true, "Watcher hit but managed service is %s", ms.State())
			return
		}

		// lock LOCAL mutex
		mu.Lock()
		defer mu.Unlock()

		// test for change
		if bp.Equal(last) {
			return
		}

		// record updated index
		last = bp

		// attempt to push to update chan.  this _should_ never block, if it does complain about it.
		select {
		case up <- bp:
		default:
			// needed to ensure clean stop
			ms.logf(false, "Watcher unable to push to update chan")
		}
	}

	return wp, nil
}

// runWatchPlan runs the current watch plan, pushing its on-stop error to the provided channel
func (ms *ManagedService) runWatchPlan(wp *watch.Plan, stopped chan<- error) {
	var (
		logger *log.Logger
		err    error
	)

	// acquire lock for notification push
	ms.mu.RLock()
	ms.pushNotification(NotificationEventManagedServiceWatchPlanStarted, ms.buildUpdate(nil))
	// must unlock here, as the next statement is blocking
	ms.mu.RUnlock()

	// build logger and run watch plan.
	if ms.logger == nil {
		logger = log.New(ioutil.Discard, "", 0)
	} else {
		logger = log.New(&loggerWriter{ms.logger}, "", log.LstdFlags)
	}

	// blocks until watch plan stops
	err = wp.RunWithClientAndLogger(ms.client, logger)

	// once again acquire rlock for notification push
	ms.mu.RLock()
	ms.pushNotification(NotificationEventManagedServiceWatchPlanStopped, ms.buildUpdate(err))
	ms.mu.RUnlock()

	select {
	case stopped <- err:

	default:
		ms.logf(false, "Watcher unable to push to stopped chan")
	}
}

func (ms *ManagedService) buildAndRunWatchPlan(up chan<- watch.WaitIndexVal, stopped chan<- error) (*watch.Plan, error) {
	var (
		wp  *watch.Plan
		err error
	)
	if wp, err = ms.buildWatchPlan(up); err == nil {
		go ms.runWatchPlan(wp, stopped)
	}

	return wp, err
}

func (ms *ManagedService) maintainForceRefresh(ch chan error) {
	ctx, cancel := context.WithTimeout(context.Background(), ms.rttl)
	defer cancel()
	qm, err := ms.refreshService(ctx)
	if err != nil {
		ms.logf(false, "maintainLock() - forceRefresh failed. err: %s; QueryMeta: %v", err, qm)
	}
	ch <- err
}

func (ms *ManagedService) maintainWatchPlanUpdate(idx watch.WaitIndexVal) {
	ctx, cancel := context.WithTimeout(context.Background(), ms.rttl)
	defer cancel()
	if qm, err := ms.refreshService(ctx); err != nil {
		ms.logf(false, "maintainLock() - Error refreshing service after watch plan update (%d). err %s; QueryMeta %v", idx, err, qm)
	} else {
		ms.logf(true, "maintainLock() - Service updated successfully after watch plan update hit (%d)", idx)
	}
}

func (ms *ManagedService) maintainRefreshTimerTick() {
	ctx, cancel := context.WithTimeout(context.Background(), ms.rttl)
	defer cancel()
	if qm, err := ms.refreshService(ctx); err != nil {
		ms.logf(false, "maintainLock() - refresh failed. err: %s; QueryMeta: %v", err, qm)
	}
}

func (ms *ManagedService) maintain() {
	var (
		wp  *watch.Plan
		err error

		wpUpdate     = make(chan watch.WaitIndexVal, 5) // TODO: do more fun stuff...
		wpStopped    = make(chan error, 1)
		refreshTimer = time.NewTimer(ms.refreshInterval)
	)

	ms.logf(true, "maintainLock() - building initial watch plan...")

	if wp, err = ms.buildAndRunWatchPlan(wpUpdate, wpStopped); err != nil {
		ms.logf(false, "maintainLock() - error building initial watch plan: %s", err)
	}

	ms.logf(true, "maintainLock() - entering loop")

	for {
		select {
		case frch := <-ms.forceRefresh:
			ms.logf(true, "maintainLock() - forceRefresh hit")

			ms.mu.Lock()
			ms.maintainForceRefresh(frch)
			ms.mu.Unlock()

			if !refreshTimer.Stop() && len(refreshTimer.C) > 0 {
				<-refreshTimer.C
			}
			refreshTimer.Reset(ms.refreshInterval)

		case err := <-wpStopped:
			ms.logf(false, "maintainLock() - Watch plan stopped with error: %s", err)
			if wp, err = ms.buildAndRunWatchPlan(wpUpdate, wpStopped); err != nil {
				ms.logf(false, "maintainLock() - Error building watch plan after stop: %s", err)
			} else {
				ms.logf(false, "maintainLock() - Watch plan successfully rebuilt, running...")
			}

		case idx := <-wpUpdate:
			ms.logf(true, "maintainLock() - Watch plan has received update (idx: %v)", idx)

			ms.mu.Lock()
			ms.maintainWatchPlanUpdate(idx)
			ms.mu.Unlock()

			if !refreshTimer.Stop() && len(refreshTimer.C) > 0 {
				<-refreshTimer.C
			}
			refreshTimer.Reset(ms.refreshInterval)

		case tick := <-refreshTimer.C:
			ms.logf(true, "maintainLock() - refreshTimer hit (%s)", tick)

			ms.mu.Lock()

			ms.maintainRefreshTimerTick()

			// check for the watch plan being nil here, and attempt to start if so
			if wp == nil {
				ms.logf(false, "maintainLock() - Watch plan is nil, attempting to rebuild...")
				if wp, err = ms.buildAndRunWatchPlan(wpUpdate, wpStopped); err != nil {
					ms.logf(false, "maintainLock() - Error building watch plan during refresh: %s", err)
				} else {
					ms.logf(false, "maintainLock() - Watch plan successfully rebuilt")
				}
			} else {
				ms.logf(true, "maintainLock() - Watch plan is still running, hooray.")
			}

			ms.mu.Unlock()

			refreshTimer.Reset(ms.refreshInterval)

		case drop := <-ms.stop:

			// locking is not necessary here as we only directly interact with function-local variables and further
			// instruction is prevented by the state setting methods that must be called in order for this case to be
			// hit

			var err error

			ms.logf(false, "maintainLock() - Stop hit")

			// stop watcher
			wp.Stop()

			// wait for goroutine to end
			<-wpStopped
			close(wpStopped)

			// close update chan, and drain if necessary.
			close(wpUpdate)
			if l := len(wpUpdate); l > 0 {
				ms.logf(true, "maintainShutdown() - Draining wpUpdate (%d items)", l)
				for range wpUpdate {
				}
			}

			// stop timer
			refreshTimer.Stop()

			// deregister service
			if err = ms.client.Agent().ServiceDeregister(ms.serviceID); err != nil {
				ms.logf(false, "maintainShutdown() - Error deregistering service: %s", err)
			} else {
				ms.logf(false, "maintainShutdown() - Service successfully deregistered")
			}

			// acquire read lock for notification push
			ms.mu.RLock()
			ms.pushNotification(NotificationEventManagedServiceStopped, ms.buildUpdate(err))
			ms.mu.RUnlock()

			drop <- err

			if !refreshTimer.Stop() || len(refreshTimer.C) > 0 {
				<-refreshTimer.C
			}

			return
		}
	}
}

// AgentServiceCheckMutator defines a callback that may mutate a new AgentServiceCheck instance
type AgentServiceCheckMutator func(*api.AgentServiceCheck)

// ManagedAgentServiceRegistration is a thin helper that provides guided construction of an AgentServiceRegistration, resulting
// in a ManagedService instance once built
type ManagedAgentServiceRegistration struct {
	api.AgentServiceRegistration
}

// ManagedAgentServiceRegistrationMutator defines a callback that may mutate a new ManagedAgentServiceRegistration instance
type ManagedAgentServiceRegistrationMutator func(*ManagedAgentServiceRegistration)

// NewManagedAgentServiceRegistration constructs a new builder based on an existing AgentServiceRegistration instance.  It
// also ensures that all slice and map fields (Tags, Checks, TaggedAddresses, and Meta) are non-nil.
//
// As a caution, the provided base is stored as-is.  If you modify the base registration type outside of the methods
// provided by the returned type, the ttlBehavior is entirely undefined.
func NewManagedAgentServiceRegistration(base *api.AgentServiceRegistration, fns ...ManagedAgentServiceRegistrationMutator) *ManagedAgentServiceRegistration {
	b := new(ManagedAgentServiceRegistration)
	if base == nil {
		base = new(api.AgentServiceRegistration)
	}
	b.AgentServiceRegistration = *base
	for _, fn := range fns {
		fn(b)
	}
	return b
}

// NewBareManagedAgentServiceRegistration constructs a new builder with the name, address, and port fields defined.  The Address
// value is guessed using LocalAddress() and may be overwritten at any time.
func NewBareManagedAgentServiceRegistration(name string, port int, fns ...ManagedAgentServiceRegistrationMutator) *ManagedAgentServiceRegistration {
	reg := new(api.AgentServiceRegistration)
	reg.Name = name
	reg.Port = port
	reg.Address, _ = LocalAddress()
	return NewManagedAgentServiceRegistration(reg, fns...)
}

// SetID set's the service's ID to the provided format string with values.  There are a few additional replacements you
// may take advantage of:
//
// 	- !RAND! will be replaced with a unique 12 character random string
// 	- !NAME! will be replaced with the service's name
// 	- !ADDR! will be replaced with the service's address
//
// Note: If you have not previously set an address or name, the slugs will be replaced with empty values
//
// Order of operation:
// 	1. fmt.Sprintf(f, v...)
//	2. All !RAND! are replaced
//  3. All !NAME! are replaced
//  4. All !ADDR! are replaced
func (b *ManagedAgentServiceRegistration) SetID(f string, v ...interface{}) *ManagedAgentServiceRegistration {
	b.ID = ReplaceSlugs(fmt.Sprintf(f, v...), SlugParams{Name: b.Name, Addr: b.Address})
	return b
}

// AddCheck will add a new check to the builder after processing all provided mutators
func (b *ManagedAgentServiceRegistration) AddCheck(check *api.AgentServiceCheck, fns ...AgentServiceCheckMutator) *ManagedAgentServiceRegistration {
	for _, fn := range fns {
		fn(check)
	}
	if b.Checks == nil {
		b.Checks = make(api.AgentServiceChecks, 0)
	}
	b.Checks = append(b.Checks, check)
	return b
}

// AddHTTPCheck compiles and adds a new HTTP service check based upon the address and port set for the service
func (b *ManagedAgentServiceRegistration) AddHTTPCheck(method, scheme, path string, interval time.Duration, fns ...AgentServiceCheckMutator) *ManagedAgentServiceRegistration {
	check := new(api.AgentServiceCheck)
	check.HTTP = fmt.Sprintf("%s://%s:%d/%s", scheme, b.Address, b.Port, strings.TrimLeft(path, "/"))
	check.Method = method
	check.Interval = interval.String()
	return b.AddCheck(check, fns...)
}

// AddTCPCheck compiles and adds a new TCP service check based upon the address and port set for the service.
func (b *ManagedAgentServiceRegistration) AddTCPCheck(interval time.Duration, fns ...AgentServiceCheckMutator) *ManagedAgentServiceRegistration {
	check := new(api.AgentServiceCheck)
	check.TCP = fmt.Sprintf("%s:%d", b.Address, b.Port)
	check.Interval = interval.String()
	return b.AddCheck(check, fns...)
}

// AddTTLCheck compiles and adds a new TTL service check.
func (b *ManagedAgentServiceRegistration) AddTTLCheck(originalStatus string, ttl time.Duration, fns ...AgentServiceCheckMutator) *ManagedAgentServiceRegistration {
	check := new(api.AgentServiceCheck)
	check.TTL = ttl.String()
	check.Status = originalStatus
	return b.AddCheck(check, fns...)
}

// AddScriptCheck compiles and adds a new script service check.
func (b *ManagedAgentServiceRegistration) AddScriptCheck(args []string, interval time.Duration, fns ...AgentServiceCheckMutator) *ManagedAgentServiceRegistration {
	check := new(api.AgentServiceCheck)
	check.Args = args
	check.Interval = interval.String()
	return b.AddCheck(check, fns...)
}

// AddDockerCheck compiles and adds a new Docker container service check.
func (b *ManagedAgentServiceRegistration) AddDockerCheck(containerID string, shell string, args []string, interval time.Duration, fns ...AgentServiceCheckMutator) *ManagedAgentServiceRegistration {
	check := new(api.AgentServiceCheck)
	check.DockerContainerID = containerID
	check.Shell = shell
	check.Args = args
	check.Interval = interval.String()
	return b.AddCheck(check, fns...)
}

// AddGRPCCheck compiles and adds a gRPC service check based upon the address and port originally configured with the
// builder.
func (b *ManagedAgentServiceRegistration) AddGRPCCheck(interval time.Duration, fns ...AgentServiceCheckMutator) *ManagedAgentServiceRegistration {
	check := new(api.AgentServiceCheck)
	check.GRPC = fmt.Sprintf("%s:%d", b.Address, b.Port)
	check.Interval = interval.String()
	return b.AddCheck(check, fns...)
}

// AddAliasCheck compiles and adds an alias service check.
func (b *ManagedAgentServiceRegistration) AddAliasCheck(service, node string, interval time.Duration, fns ...AgentServiceCheckMutator) *ManagedAgentServiceRegistration {
	check := new(api.AgentServiceCheck)
	check.AliasService = service
	check.AliasNode = node
	check.Interval = interval.String()
	return b.AddCheck(check, fns...)
}

// Create attempts to first register the configured service with the desired consul agent, then constructs a
// ManagedService instance for you to use.
//
// If no ID was specified before this method is called, one will be randomly generated for you.
func (b *ManagedAgentServiceRegistration) Create(cfg *ManagedServiceConfig) (*ManagedService, error) {
	var (
		err error

		act = new(ManagedServiceConfig)
	)

	// TODO: be less lazy here?

	if cfg == nil {
		cfg = new(ManagedServiceConfig)
	}

	*act = *cfg

	if b.ID == act.ID {
		if b.ID == "" {
			b.SetID(ServiceDefaultIDFormat)
			act.ID = b.ID
		}
	} else if b.ID == "" {
		b.ID = act.ID
	} else if act.ID == "" {
		act.ID = b.ID
	} else {
		return nil, fmt.Errorf("builder and managed service config id mismatch: %q vs %q", b.ID, act.ID)
	}

	if act.Client == nil {
		if act.Client, err = api.NewClient(api.DefaultConfig()); err != nil {
			return nil, fmt.Errorf("error creating default client: %s", err)
		}
	}

	if len(act.BaseChecks) == 0 {
		act.BaseChecks = make(api.AgentServiceChecks, 0)
		if b.Check != nil {
			act.BaseChecks = append(act.BaseChecks, b.Check)
		}
		if len(b.Checks) > 0 {
			act.BaseChecks = append(act.BaseChecks, b.Checks...)
		}
	}

	// ensure EnableTagOverride is true
	b.EnableTagOverride = true

	if err = act.Client.Agent().ServiceRegister(&b.AgentServiceRegistration); err != nil {
		return nil, fmt.Errorf("error registering service: %s", err)
	}

	return NewManagedService(act)
}
