package consultant

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/api/watch"
	"github.com/myENA/go-helpers"
)

const (
	ServiceRandSlug        = "!RAND!"
	ServiceNameSlug        = "!NAME!"
	ServiceAddrSlug        = "!ADDR!"
	ServiceDefaultIDFormat = ServiceNameSlug + "-" + ServiceAddrSlug + "-" + ServiceRandSlug

	ServiceDefaultRefreshInterval    = api.ReadableDuration(30 * time.Second)
	ServiceDefaultInternalRequestTTL = 2 * time.Second
)

var (
	svcRandReplaceRegexp = regexp.MustCompile("(" + ServiceRandSlug + ")")
	svcNameReplaceRegexp = regexp.MustCompile("(" + ServiceNameSlug + ")")
	svcAddrReplaceRegexp = regexp.MustCompile("(" + ServiceAddrSlug + ")")
)

// ManagedServiceBuilderMutator defines a callback that may mutate a new ManagedServiceBuilder instance
type ManagedServiceBuilderMutator func(*ManagedServiceBuilder)

// ManagedServiceCheckMutator defines a callback that may mutate a new AgentServiceCheck instance
type ManagedServiceCheckMutator func(*api.AgentServiceCheck)

// ManagedServiceConfig describes the basis for a new ManagedService instance
type ManagedServiceConfig struct {
	// ID [required]
	//
	// ID of service to fetch to turn into a managed service
	ID string `json:"name" hcl:"name"`

	// BaseChecks [optional] (recommended)
	//
	// These are the base service checks that will be re-registered with the service should it be removed exterenaly
	// from the node the service was registered to.
	//
	// This will most likely be removed in the future once https://github.com/hashicorp/consul/issues/1680 is finally
	// implemented, but for now is necessary if you wish for your service to automatically have its health checks
	// re-registered
	BaseChecks api.AgentServiceChecks `json:"base_checks" hcl:"base_checks"`

	// RefreshInterval [optional]
	//
	// Optionally specify a refresh interval.  Defaults to value of ServiceDefaultRefreshInterval.
	RefreshInterval api.ReadableDuration `json:"refresh_interval" hcl:"refresh_interval"`

	// QueryOptions [optional]
	//
	// Options to use whenever making a read api query.  This will be shallow copied per internal request made.
	QueryOptions *api.QueryOptions `json:"query_options" hcl:"query_options"`

	// WriteOptions [optional]
	//
	// Options to use whenever making a write api query.  This will be shallow copied per internal request made.
	WriteOptions *api.WriteOptions `json:"write_options" hcl:"write_options"`

	// RequestTTL [optional]
	//
	// Optionally specify a TTL to pass to internal API requests.  Defaults to value of ServiceDefaultInternalRequestTTL
	RequestTTL time.Duration `json:"request_ttl" hcl:"request_ttl"`

	// Debug [optional]
	//
	// If true, will enable debug-level logging if a logger is provided
	Debug bool `json:"debug" hcl:"debug"`

	// APIConfig [optional]
	//
	// Optionally provide a Consul API Client configuration type to use when building the internal API client instance.
	// If not provided the client will be built using api.DefaultConfig()
	APIConfig *api.Config `json:"api_config" hcl:"api_config"`
}

// ManagedService
//
// This type is a wrapper around an existing Consul agent service.  It provides several wrappers to make managing the
// lifecycle of a processes' service registration easier.
type ManagedService struct {
	mu sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc

	rttl time.Duration

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

	done chan struct{}

	dbg    bool
	logger Logger
}

// NewManagedService creates a new ManagedService instance.  If no logger is provided, all logging will be silenced
func NewManagedService(ctx context.Context, cfg *ManagedServiceConfig, logger Logger) (*ManagedService, error) {
	var (
		apiConfig *api.Config
		err       error

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
	if cfg.APIConfig != nil {
		apiConfig = cfg.APIConfig
	} else {
		apiConfig = api.DefaultConfig()
	}

	if ms.client, err = api.NewClient(apiConfig); err != nil {
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

	// set refresh interval
	if cfg.RefreshInterval != 0 {
		ms.refreshInterval = cfg.RefreshInterval.Duration()
	} else {
		ms.refreshInterval = time.Duration(ServiceDefaultRefreshInterval)
	}

	// maybe log
	ms.dbg = cfg.Debug
	ms.logger = logger

	// misc
	ms.ctx, ms.cancel = context.WithCancel(ctx)
	if cfg.RequestTTL > 0 {
		ms.rttl = cfg.RequestTTL
	} else {
		ms.rttl = ServiceDefaultInternalRequestTTL
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

func (ms *ManagedService) maintain() {
	var (
		wp  *watch.Plan
		err error

		wpStopped    = make(chan error, 1)
		wpUpdate     = make(chan watch.WaitIndexVal, 5) // TODO: do more fun stuff...
		refreshTimer = time.NewTimer(ms.refreshInterval)
	)

	ms.logf(true, "maintain() - building initial watch plan...")

	if wp, err = ms.buildWatchPlan(wpUpdate); err != nil {
		ms.logf(false, "maintain() - error building initial watch plan: %s", err)
	} else {
		go ms.runWatchPlan(wp, wpStopped)
		ms.logf(true, "maintain() - initial watch plan built and running")
	}

	ms.logf(true, "maintain() - entering loop")

	defer func() {
		ms.logf(false, "maintain() - exiting loop")

		// always cancel...
		ms.cancel()

		// stop watcher
		wp.Stop()

		// stop timer
		refreshTimer.Stop()

		// deregister service
		if err := ms.client.Agent().ServiceDeregister(ms.serviceID); err != nil {
			ms.logf(false, "maintain() - Error deregistering service: %s", err)
		} else {
			ms.logf(false, "maintain() - Service successfully deregistered")
		}

		// channel cleanup
		close(ms.forceRefresh)
		close(wpStopped)
		close(wpUpdate)
		close(ms.done)
	}()

	for {
		select {
		case ch := <-ms.forceRefresh:
			ms.logf(true, "maintain() - forceRefresh hit")
			refreshTimer.Stop()
			ms.mu.Lock()
			ctx, cancel := context.WithTimeout(ms.ctx, ms.rttl)
			qm, err := ms.refreshService(ctx)
			if err != nil {
				ms.logf(false, "maintain() - forceRefresh failed. err: %s; QueryMeta: %v", err, qm)
			}
			cancel()
			ms.mu.Unlock()
			ch <- err
			refreshTimer = time.NewTimer(ms.refreshInterval)

		case <-wpStopped:
			ms.logf(false, "maintain() - Watch plan stopped with error: %s")
			if wp, err = ms.buildAndRunWatchPlan(wpUpdate, wpStopped); err != nil {
				ms.logf(false, "maintain() - Error building watch plan after stop: %s", err)
			} else {
				ms.logf(false, "maintain() - Watch plan successfully rebuilt, running...")
			}

		case idx := <-wpUpdate:
			ms.logf(true, "maintain() - Watch plan has received update (idx: %v)", idx)
			refreshTimer.Stop()
			ms.mu.Lock()
			ctx, cancel := context.WithTimeout(ms.ctx, ms.rttl)
			if qm, err := ms.refreshService(ctx); err != nil {
				ms.logf(false, "maintain() - Error refreshing service after watch plan update. err %s; QueryMeta %v", err, qm)
			} else {
				ms.logf(true, "maintain() - Service updated successfully after watch plan update hit")
			}
			cancel()
			ms.mu.Unlock()
			refreshTimer = time.NewTimer(ms.refreshInterval)

		case t := <-refreshTimer.C:
			ms.logf(true, "maintain() - refreshTimer hit (%s)", t)
			ms.mu.Lock()
			ctx, cancel := context.WithTimeout(ms.ctx, ms.rttl)
			if qm, err := ms.refreshService(ctx); err != nil {
				ms.logf(false, "maintain() - refresh failed. err: %s; QueryMeta: %v", err, qm)
			}
			cancel()
			ms.mu.Unlock()
			if wp == nil {
				ms.logf(false, "maintain() - Watch plan is nil, attempting to rebuild...")
				if wp, err = ms.buildAndRunWatchPlan(wpUpdate, wpStopped); err != nil {
					ms.logf(false, "maintain() - Error building watch plan during refresh: %s", err)
				} else {
					ms.logf(false, "maintain() - Watch plan successfully rebuilt")
				}
			} else {
				ms.logf(true, "maintain() - Watch plan is still running, hooray.")
			}
			refreshTimer = time.NewTimer(ms.refreshInterval)

		case <-ms.ctx.Done():
			ms.logf(true, "maintain() - Internal context closed: %s", ms.ctx.Err())
			return
		}
	}
}

// ManagedServiceBuilder is a thin helper that provides guided construction of an AgentServiceRegistration, resulting
// in a ManagedService instance once built
type ManagedServiceBuilder struct {
	*api.AgentServiceRegistration
}

// NewManagedServiceBuilder constructs a new builder based on an existing AgentServiceRegistration instance.  It
// also ensures that all slice and map fields (Tags, Checks, TaggedAddresses, and Meta) are non-nil.
//
// As a caution, the provided base is stored as-is.  If you modify the base registration type outside of the methods
// provided by the returned type, the behavior is entirely undefined.
func NewManagedServiceBuilder(base *api.AgentServiceRegistration, fns ...ManagedServiceBuilderMutator) *ManagedServiceBuilder {
	b := new(ManagedServiceBuilder)
	if base == nil {
		base = new(api.AgentServiceRegistration)
	}
	b.AgentServiceRegistration = base
	for _, fn := range fns {
		fn(b)
	}
	return b
}

// NewBareManagedServiceBuilder constructs a new builder with the name, address, and port fields defined.  The Address
// value is guessed using LocalAddress() and may be overwritten at any time.
func NewBareManagedServiceBuilder(name string, port int, fns ...ManagedServiceBuilderMutator) *ManagedServiceBuilder {
	reg := new(api.AgentServiceRegistration)
	reg.Name = name
	reg.Port = port
	reg.Address, _ = LocalAddress()
	return NewManagedServiceBuilder(reg, fns...)
}

// SetID set's the service's ID to the provided format string with values.  There are a few additional replacements you
// may take advantage of:
//
// 	- !RAND! will be replaced with a unique 12 character random string
// 	- !NAME! will be replaced with the service's name
// 	- !ADDR! will be replaced with the service's address
//
// Order of operation:
// 	1. fmt.Sprintf(f, v...)
//	2. All !RAND! are replaced
//  3. All !NAME! are replaced
//  4. All !ADDR! are replaced
func (b *ManagedServiceBuilder) SetID(f string, v ...interface{}) *ManagedServiceBuilder {
	if b.AgentServiceRegistration == nil {
		b.AgentServiceRegistration = new(api.AgentServiceRegistration)
	}
	b.ID = svcAddrReplaceRegexp.ReplaceAllStringFunc(
		svcNameReplaceRegexp.ReplaceAllStringFunc(
			svcRandReplaceRegexp.ReplaceAllStringFunc(
				fmt.Sprintf(f, v...),
				func(_ string) string { return LazyRandomString(12) },
			),
			func(_ string) string { return b.Name },
		),
		func(_ string) string { return b.Address },
	)
	return b
}

// AddCheck will add a new check to the builder after processing all provided mutators
func (b *ManagedServiceBuilder) AddCheck(check *api.AgentServiceCheck, fns ...ManagedServiceCheckMutator) *ManagedServiceBuilder {
	if b.AgentServiceRegistration == nil {
		b.AgentServiceRegistration = new(api.AgentServiceRegistration)
	}
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
func (b *ManagedServiceBuilder) AddHTTPCheck(method, scheme, path string, interval time.Duration, fns ...ManagedServiceCheckMutator) *ManagedServiceBuilder {
	if b.AgentServiceRegistration == nil {
		b.AgentServiceRegistration = new(api.AgentServiceRegistration)
	}
	check := new(api.AgentServiceCheck)
	check.HTTP = fmt.Sprintf("%s://%s:%d/%s", scheme, b.Address, b.Port, strings.TrimLeft(path, "/"))
	check.Method = method
	check.Interval = interval.String()
	return b.AddCheck(check, fns...)
}

// AddTCPCheck compiles and adds a new TCP service check based upon the address and port set for the service.
func (b *ManagedServiceBuilder) AddTCPCheck(interval time.Duration, fns ...ManagedServiceCheckMutator) *ManagedServiceBuilder {
	if b.AgentServiceRegistration == nil {
		b.AgentServiceRegistration = new(api.AgentServiceRegistration)
	}
	check := new(api.AgentServiceCheck)
	check.TCP = fmt.Sprintf("%s:%d", b.Address, b.Port)
	check.Interval = interval.String()
	return b.AddCheck(check, fns...)
}

// AddTTLCheck compiles and adds a new TTL service check.
func (b *ManagedServiceBuilder) AddTTLCheck(originalStatus string, ttl time.Duration, fns ...ManagedServiceCheckMutator) *ManagedServiceBuilder {
	if b.AgentServiceRegistration == nil {
		b.AgentServiceRegistration = new(api.AgentServiceRegistration)
	}
	check := new(api.AgentServiceCheck)
	check.TTL = ttl.String()
	check.Status = originalStatus
	return b.AddCheck(check, fns...)
}

// AddScriptCheck compiles and adds a new script service check.
func (b *ManagedServiceBuilder) AddScriptCheck(args []string, interval time.Duration, fns ...ManagedServiceCheckMutator) *ManagedServiceBuilder {
	if b.AgentServiceRegistration == nil {
		b.AgentServiceRegistration = new(api.AgentServiceRegistration)
	}
	check := new(api.AgentServiceCheck)
	check.Args = args
	check.Interval = interval.String()
	return b.AddCheck(check, fns...)
}

// AddDockerCheck compiles and adds a new Docker container service check.
func (b *ManagedServiceBuilder) AddDockerCheck(containerID string, shell string, args []string, interval time.Duration, fns ...ManagedServiceCheckMutator) *ManagedServiceBuilder {
	if b.AgentServiceRegistration == nil {
		b.AgentServiceRegistration = new(api.AgentServiceRegistration)
	}
	check := new(api.AgentServiceCheck)
	check.DockerContainerID = containerID
	check.Shell = shell
	check.Args = args
	check.Interval = interval.String()
	return b.AddCheck(check, fns...)
}

// AddGRPCCheck compiles and adds a gRPC service check based upon the address and port originally configured with the
// builder.
func (b *ManagedServiceBuilder) AddGRPCCheck(interval time.Duration, fns ...ManagedServiceCheckMutator) *ManagedServiceBuilder {
	if b.AgentServiceRegistration == nil {
		b.AgentServiceRegistration = new(api.AgentServiceRegistration)
	}
	check := new(api.AgentServiceCheck)
	check.GRPC = fmt.Sprintf("%s:%d", b.Address, b.Port)
	check.Interval = interval.String()
	return b.AddCheck(check, fns...)
}

// AddAliasCheck compiles and adds an alias service check.
func (b *ManagedServiceBuilder) AddAliasCheck(service, node string, interval time.Duration, fns ...ManagedServiceCheckMutator) *ManagedServiceBuilder {
	if b.AgentServiceRegistration == nil {
		b.AgentServiceRegistration = new(api.AgentServiceRegistration)
	}
	check := new(api.AgentServiceCheck)
	check.AliasService = service
	check.AliasNode = node
	check.Interval = interval.String()
	return b.AddCheck(check, fns...)
}

// Build attempts to first register the configured service with the desired consul agent, then constructs a
// ManagedService instance for you to use.
//
// The context parameter in this instance will be used to maintain the state of the created ManagedService instance.
// Cancelling the context will terminate the ManagedService, making it defunct and removing the service from the consul
// agent.
//
// If no ID was specified before this method is called, one will be randomly generated for you.
func (b *ManagedServiceBuilder) Build(serviceCtx context.Context, cfg *ManagedServiceConfig, logger Logger) (*ManagedService, error) {
	if b.AgentServiceRegistration == nil {
		b.AgentServiceRegistration = new(api.AgentServiceRegistration)
	}

	var (
		client *api.Client
		err    error
	)

	if cfg == nil {
		cfg = new(ManagedServiceConfig)
		cfg.RefreshInterval = ServiceDefaultRefreshInterval
	}

	if len(cfg.BaseChecks) == 0 {
		cfg.BaseChecks = make(api.AgentServiceChecks, 0)
		if b.Check != nil {
			cfg.BaseChecks = append(cfg.BaseChecks, b.Check)
		}
		if len(b.Checks) > 0 {
			cfg.BaseChecks = append(cfg.BaseChecks, b.Checks...)
		}
	}

	if b.ID == cfg.ID {
		if b.ID == "" {
			b.SetID(ServiceDefaultIDFormat)
			cfg.ID = b.ID
		}
	} else if b.ID == "" {
		b.ID = cfg.ID
	} else if cfg.ID == "" {
		cfg.ID = b.ID
	} else {
		return nil, fmt.Errorf("builder and managed service config id mismatch: %q vs %q", b.ID, cfg.ID)
	}

	// ensure we have a tag with the id of the service
	var found bool
	if b.Tags == nil {
		b.Tags = make([]string, 1)
	} else {
		for _, v := range b.Tags {
			if v == b.ID {
				found = true
				break
			}
		}
	}
	if !found {
		b.Tags = append(b.Tags, b.ID)
	}

	if cfg.APIConfig == nil {
		cfg.APIConfig = api.DefaultConfig()
	}
	if client, err = api.NewClient(cfg.APIConfig); err != nil {
		return nil, fmt.Errorf("error creating default client: %s", err)
	}

	// ensure EnableTagOverride is true
	b.EnableTagOverride = true

	if err = client.Agent().ServiceRegister(b.AgentServiceRegistration); err != nil {
		return nil, fmt.Errorf("error registering service: %s", err)
	}

	return NewManagedService(serviceCtx, cfg, logger)
}
