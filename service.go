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

const (
	ServiceDefaultIDFormat        = SlugName + "-" + SlugAddr + "-" + SlugRand
	ServiceDefaultRefreshInterval = api.ReadableDuration(30 * time.Second)
)

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
	mu sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc

	serviceID   string
	baseChecks  api.AgentServiceChecks
	tagOverride bool

	svc             *api.AgentService
	refreshInterval time.Duration
	localRefreshed  time.Time
	forceRefresh    chan chan error

	client *api.Client
	qo     *api.QueryOptions
	wo     *api.WriteOptions
	rttl   time.Duration

	done chan struct{}

	dbg    bool
	logger Logger
}

// NewManagedService creates a new ManagedService instance.
func NewManagedService(ctx context.Context, cfg *ManagedServiceConfig) (*ManagedService, error) {
	var (
		err error

		ms = new(ManagedService)
	)

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

	// ensure we have a context of some kind
	if ctx == nil {
		ctx = context.Background()
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
		if l := len(cfg.QueryOptions.NodeMeta); l > 0 {
			ms.qo.NodeMeta = make(map[string]string, l)
			for k, v := range cfg.QueryOptions.NodeMeta {
				ms.qo.NodeMeta[k] = v
			}
		}
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

	// maybe log
	ms.dbg = cfg.Debug
	ms.logger = cfg.Logger

	// misc
	ms.ctx, ms.cancel = context.WithCancel(ctx)
	if cfg.RequestTTL > 0 {
		ms.rttl = cfg.RequestTTL
	} else {
		ms.rttl = defaultInternalRequestTTL
	}
	ms.forceRefresh = make(chan chan error)
	ms.done = make(chan struct{})

	// fetch initial service state from node
	ctx, cancel := context.WithTimeout(ms.ctx, ms.rttl)
	defer cancel()
	if _, err = ms.refreshService(ctx); err != nil {
		return nil, fmt.Errorf("error fetching current state of service: %s", err)
	}

	ms.tagOverride = ms.svc.EnableTagOverride

	go ms.maintain()

	return ms, nil
}

// LastRefreshed is the last time the internal state of the managed service was last updated
func (ms *ManagedService) LastRefreshed() time.Time {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return ms.localRefreshed
}

// Done returns the internal context .Done chan
func (ms *ManagedService) Done() <-chan struct{} {
	return ms.ctx.Done()
}

// Err returns the current error of the internal context
func (ms *ManagedService) Err() error {
	return ms.ctx.Err()
}

// Deregister stops the internal context, ceasing all operations chained from it and blocking until the service has been
// deregistered from the connected agent.
func (ms *ManagedService) Deregister() {
	if err := ms.Err(); err == nil {
		ms.cancel()
		<-ms.done
		return
	}
}

// AgentService attempts to fetch the current AgentService entry for this ManagedService using Agent().Service()
func (ms *ManagedService) AgentService(ctx context.Context) (*api.AgentService, *api.QueryMeta, error) {
	if err := ms.Err(); err != nil {
		return nil, nil, err
	}
	return ms.client.Agent().Service(ms.serviceID, ms.qo.WithContext(ctx))
}

// HealthServiceEntry attempts to fetch the current ServiceEntry entry for this ManagedService using Health().Service()
func (ms *ManagedService) HealthServiceEntry(ctx context.Context) (*api.ServiceEntry, *api.QueryMeta, error) {
	if err := ms.Err(); err != nil {
		return nil, nil, err
	}
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return ms.findHealth(ctx)
}

// CatalogService attempts to fetch the current CatalogService entry for this ManagedService using Catalog().Service
func (ms *ManagedService) CatalogService(ctx context.Context) (*api.CatalogService, *api.QueryMeta, error) {
	if err := ms.Err(); err != nil {
		return nil, nil, err
	}
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return ms.findCatalog(ctx)
}

// Checks returns a list of all the checks registered to this specific service
func (ms *ManagedService) Checks(ctx context.Context) (api.HealthChecks, *api.QueryMeta, error) {
	if err := ms.Err(); err != nil {
		return nil, nil, err
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
// If no tags were provided or the provided list of tags are already present on the service a 0, nil will be returned
func (ms *ManagedService) AddTags(ctx context.Context, tags ...string) (int, error) {
	if err := ms.Err(); err != nil {
		return 0, err
	}
	if !ms.tagOverride {
		return 0, errors.New("cannot add tags: EnableTagOverride was false at service registration")
	}
	ms.logf(true, "AddTags() - Adding tags %v to service %q...", tags, ms.serviceID)
	if len(tags) == 0 {
		ms.logf(true, "AddTags() - provided tag input empty")
		return 0, nil
	}
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	var (
		svc     *api.CatalogService
		newTags []string
		added   int
		err     error
	)
	if svc, _, err = ms.findCatalog(ctx); err != nil {
		return 0, err
	}
	newTags, added = helpers.CombineStringSlices(svc.ServiceTags, tags)
	if added == 0 {
		ms.logf(true, "AddTags() - no new tags provided")
		return 0, nil
	}
	if err = ms.registerService(newTags); err != nil {
		return 0, err
	}
	return added, nil
}

// RemoveTags attempts to remove one or more tags from the service registration in consul, if and only if
// EnableTagOverride was enabled when the service was registered.
//
// Returns:
//	- count of tags removed
// 	- error, if unsuccessful
//
// If no tags were provided or none of the tags in the list were present on the service, a 0, nil will be returned.
func (ms *ManagedService) RemoveTags(ctx context.Context, tags ...string) (int, error) {
	if err := ms.Err(); err != nil {
		return 0, err
	}
	if !ms.tagOverride {
		return 0, errors.New("cannot remove tags: EnableTagOverride was false at service registration")
	}
	ms.logf(true, "RemoveTags() - Removing tags %v from service %q...", tags, ms.serviceID)
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	var (
		svc     *api.CatalogService
		newTags []string
		removed int
		err     error
	)
	if svc, _, err = ms.findCatalog(ctx); err != nil {
		return 0, err
	}
	newTags, removed = helpers.RemoveStringsFromSlice(svc.ServiceTags, tags)
	if removed == 0 {
		ms.logf(true, "RemoveTags() - Service does not have any tags from input %v", tags)
		return 0, nil
	}
	if err = ms.registerService(newTags); err != nil {
		return 0, err
	}
	return removed, nil
}

// ForceRefresh attempts an immediate internal state refresh, blocking until attempt has been completed.
func (ms *ManagedService) ForceRefresh() error {
	if err := ms.Err(); err != nil {
		return err
	}
	ms.logf(true, "ForceRefresh() - request received...")
	ch := make(chan error, 1)
	defer close(ch)
	ms.forceRefresh <- ch
	return <-ch
}

func (ms *ManagedService) logf(debug bool, f string, v ...interface{}) {
	if ms.logger == nil || debug && !ms.dbg {
		return
	}
	ms.logger.Printf(f, v...)
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
// caller must hold lock
func (ms *ManagedService) refreshService(ctx context.Context) (*api.QueryMeta, error) {
	var (
		svc *api.AgentService
		qm  *api.QueryMeta
		err error
	)
	ms.logf(true, "refreshService() - Refreshing local service...")
	if svc, qm, err = ms.AgentService(ctx); err != nil {
		if !IsNotFoundError(err) {
			ms.logf(false, "refreshService() - Error fetching service from node: %s", err)
			return qm, err
		}
		ms.logf(false, "refreshService() - Received 404 not found, attempting to re-register...")
		if err = ms.registerService(ms.svc.Tags); err != nil {
			ms.logf(false, "refreshService() - Failed to re-register service: %s")
			return nil, err
		}
		ms.logf(false, "refreshService() - Service successfully re-registered")
	}
	ms.svc = svc
	ms.logf(true, "refreshService() - Service refreshed: %v", svc)
	ms.localRefreshed = time.Now()
	return qm, nil
}

// registerService will attempt to re-push the service to the consul agent
//
// caller must hold lock
func (ms *ManagedService) registerService(tags []string) error {
	ms.logf(false, "registerService() - Registering service with node...")

	reg := new(api.AgentServiceRegistration)
	reg.Kind = ms.svc.Kind
	reg.ID = ms.svc.ID
	reg.Name = ms.svc.Service
	reg.Tags = tags
	reg.Port = ms.svc.Port
	reg.Address = ms.svc.Address
	reg.TaggedAddresses = ms.svc.TaggedAddresses
	reg.EnableTagOverride = ms.svc.EnableTagOverride
	reg.Meta = ms.svc.Meta
	reg.Weights = &ms.svc.Weights
	reg.Checks = ms.baseChecks
	reg.Proxy = ms.svc.Proxy
	reg.Connect = ms.svc.Connect

	if err := ms.client.Agent().ServiceRegister(reg); err != nil {
		ms.logf(false, "registerService() - Error registering service: %s", err)
		return err
	}

	return nil
}

// buildWatchPlan constructs a new watch plan with appropriate handler defined
func (ms *ManagedService) buildWatchPlan(up chan<- watch.WaitIndexVal) (*watch.Plan, error) {
	var (
		token, datacenter string
		mu                sync.Mutex
		last              watch.WaitIndexVal
	)
	if ms.qo != nil {
		token = ms.qo.Token
		datacenter = ms.qo.Datacenter
	}
	wp, err := WatchServiceMultipleTags(ms.svc.Service, ms.svc.Tags, false, true, token, datacenter)
	if err != nil {
		return nil, err
	}
	wp.HybridHandler = func(val watch.BlockingParamVal, svcs interface{}) {
		bp, ok := val.(watch.WaitIndexVal)
		if !ok {
			ms.logf(false, "Watcher expected val to be of type watch.WaitIndexVal, saw %T", val)
			return
		}
		if ms.ctx.Err() != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if bp.Equal(last) {
			bp = last
			return
		}
		last = bp
		select {
		case up <- bp:
		default:
			ms.logf(false, "Watcher unable to push to update chan")
		}
	}
	return wp, nil
}

func (ms *ManagedService) runWatchPlan(wp *watch.Plan, stopped chan<- error) {
	// build logger and run watch plan.
	var logger *log.Logger
	if ms.logger == nil {
		logger = log.New(ioutil.Discard, "", 0)
	} else {
		logger = log.New(&loggerWriter{ms.logger}, "", log.LstdFlags)
	}
	err := wp.RunWithClientAndLogger(ms.client, logger)
	if ms.ctx.Err() != nil {
		return
	}
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
	ms.mu.Lock()

	ctx, cancel := context.WithTimeout(ms.ctx, ms.rttl)
	defer cancel()
	qm, err := ms.refreshService(ctx)
	if err != nil {
		ms.logf(false, "maintainLock() - forceRefresh failed. err: %s; QueryMeta: %v", err, qm)
	}

	ms.mu.Unlock()
	ch <- err
}

func (ms *ManagedService) maintainWatchPlanUpdate(idx watch.WaitIndexVal) {
	ms.mu.Lock()

	ctx, cancel := context.WithTimeout(ms.ctx, ms.rttl)
	defer cancel()
	if qm, err := ms.refreshService(ctx); err != nil {
		ms.logf(false, "maintainLock() - Error refreshing service after watch plan update. err %s; QueryMeta %v", err, qm)
	} else {
		ms.logf(true, "maintainLock() - Service updated successfully after watch plan update hit")
	}

	ms.mu.Unlock()
}

func (ms *ManagedService) maintainRefreshTimerTick() {
	ms.mu.Lock()

	ctx, cancel := context.WithTimeout(ms.ctx, ms.rttl)
	defer cancel()
	if qm, err := ms.refreshService(ctx); err != nil {
		ms.logf(false, "maintainLock() - refresh failed. err: %s; QueryMeta: %v", err, qm)
	}

	ms.mu.Unlock()
}

func (ms *ManagedService) maintainShutdown(refreshTimer *time.Timer, wp *watch.Plan, wpUpdate chan watch.WaitIndexVal, wpStopped chan error) {
	ms.logf(false, "maintainLock() - loop exited")

	// always cancel...
	ms.cancel()

	// stop watcher
	wp.Stop()

	// stop timer
	refreshTimer.Stop()

	// deregister service
	if err := ms.client.Agent().ServiceDeregister(ms.serviceID); err != nil {
		ms.logf(false, "maintainLock() - Error deregistering service: %s", err)
	} else {
		ms.logf(false, "maintainLock() - Service successfully deregistered")
	}

	// channel cleanup
	close(ms.forceRefresh)
	close(wpStopped)
	close(wpUpdate)
	close(ms.done)
}

func (ms *ManagedService) maintain() {
	var (
		tick time.Time
		idx  watch.WaitIndexVal
		frch chan error
		wp   *watch.Plan
		err  error

		wpUpdate     = make(chan watch.WaitIndexVal, 5) // TODO: do more fun stuff...
		wpStopped    = make(chan error, 1)
		refreshTimer = time.NewTimer(ms.refreshInterval)
	)

	ms.logf(true, "maintainLock() - building initial watch plan...")

	if wp, err = ms.buildWatchPlan(wpUpdate); err != nil {
		ms.logf(false, "maintainLock() - error building initial watch plan: %s", err)
	} else {
		go ms.runWatchPlan(wp, wpStopped)
		ms.logf(true, "maintainLock() - initial watch plan built and running")
	}

	ms.logf(true, "maintainLock() - entering loop")

	// queue up shutdown op
	defer ms.maintainShutdown(refreshTimer, wp, wpUpdate, wpStopped)

	for {
		select {
		case frch = <-ms.forceRefresh:
			refreshTimer.Stop()
			ms.logf(true, "maintainLock() - forceRefresh hit")
			ms.maintainForceRefresh(frch)
			refreshTimer.Reset(ms.refreshInterval)

		case <-wpStopped:
			ms.logf(false, "maintainLock() - Watch plan stopped with error: %s")
			if wp, err = ms.buildAndRunWatchPlan(wpUpdate, wpStopped); err != nil {
				ms.logf(false, "maintainLock() - Error building watch plan after stop: %s", err)
			} else {
				ms.logf(false, "maintainLock() - Watch plan successfully rebuilt, running...")
			}

		case idx = <-wpUpdate:
			refreshTimer.Stop()
			ms.logf(true, "maintainLock() - Watch plan has received update (idx: %v)", idx)
			ms.maintainWatchPlanUpdate(idx)
			refreshTimer.Reset(ms.refreshInterval)

		case tick = <-refreshTimer.C:
			ms.logf(true, "maintainLock() - refreshTimer hit (%s)", tick)
			ms.maintainRefreshTimerTick()
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
			refreshTimer.Reset(ms.refreshInterval)

		case <-ms.ctx.Done():
			ms.logf(true, "maintainLock() - Internal context closed: %s", ms.ctx.Err())
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
// The context parameter in this instance will be used to maintainLock the state of the created ManagedService instance.
// Cancelling the context will terminate the ManagedService, making it defunct and removing the service from the consul
// agent.
//
// If no ID was specified before this method is called, one will be randomly generated for you.
func (b *ManagedAgentServiceRegistration) Create(serviceCtx context.Context, cfg *ManagedServiceConfig) (*ManagedService, error) {
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

	return NewManagedService(serviceCtx, act)
}
