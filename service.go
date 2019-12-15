package consultant

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
)

const (
	ServiceRandSlug        = "!RAND!"
	ServiceNameSlug        = "!NAME!"
	ServiceAddrSlug        = "!ADDR!"
	ServiceDefaultIDFormat = ServiceNameSlug + "-" + ServiceAddrSlug + "-" + ServiceRandSlug

	ServiceDefaultRefreshInterval = api.ReadableDuration(30 * time.Second)
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
	ID string `json:"name"`

	// BaseChecks [optional] (recommended)
	//
	// These are the base service checks that will be re-registered with the service should it be removed exterenaly
	// from the node the service was registered to.
	//
	// This will most likely be removed in the future once https://github.com/hashicorp/consul/issues/1680 is finally
	// implemented, but for now is necessary if you wish for your service to automatically have its health checks
	// re-registered
	BaseChecks api.AgentServiceChecks `json:"base_checks"`

	// RefreshInterval [optional]
	//
	// Optionally specify a refresh TLL.  Defaults to value of ServiceDefaultRefreshInterval.
	RefreshInterval api.ReadableDuration `json:"refresh_ttl"`

	// QueryOptions [optional]
	//
	// Options to use whenever making a read api query.  This will be shallow copied per internal request made.
	QueryOptions *api.QueryOptions `json:"query_options"`

	// WriteOptions [optional]
	//
	// Options to use whenever making a write api query.  This will be shallow copied per internal request made.
	WriteOptions *api.WriteOptions `json:"write_options"`

	// Client [optional]
	//
	// Optionally provide a Consul client instance to use.  If one is not defined, a new one will be created with
	// default configuration values.
	Client *api.Client `json:"-"`
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

	done chan error
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

	if l := len(cfg.BaseChecks); l > 0 {
		ms.baseChecks = make(api.AgentServiceChecks, l, l)
		copy(ms.baseChecks, cfg.BaseChecks)
	} else {
		ms.baseChecks = make(api.AgentServiceChecks, 0, 0)
	}

	if ctx == nil {
		ctx = context.Background()
	}

	ms.serviceID = cfg.ID

	if cfg.Client != nil {
		ms.client = cfg.Client
	} else if ms.client, err = api.NewClient(api.DefaultConfig()); err != nil {
		return nil, fmt.Errorf("error creating client with default config: %s", err)
	}

	if err := ms.init(); err != nil {
		return nil, fmt.Errorf("error during ManagedService initialization: %s", err)
	}

	if cfg.RefreshInterval != 0 {
		ms.refreshInterval = cfg.RefreshInterval.Duration()
	} else {
		ms.refreshInterval = time.Duration(ServiceDefaultRefreshInterval)
	}

	ms.ctx, ms.cancel = context.WithCancel(ctx)
	ms.forceRefresh = make(chan chan error)
	ms.done = make(chan error)

	go ms.maintain()

	return ms, nil
}

// Done returns the internal context .Done chan
func (ms *ManagedService) Done() <-chan struct{} {
	return ms.ctx.Done()
}

// Err returns the current error of the internal context
func (ms *ManagedService) Err() error {
	return ms.ctx.Err()
}

func (ms *ManagedService) Close() error {
	if err := ms.Err(); err != nil {
		return err
	}
	ms.cancel()
	return <-ms.done
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
	var (
		svc *api.AgentService
		qm  *api.QueryMeta
		err error
	)
	if svc, qm, err = ms.AgentService(ctx); err != nil {
		return nil, qm, fmt.Errorf("error fetching service %q: %s", ms.serviceID, err)
	}
	return ms.findHealth(ctx, svc.Service, svc.Tags)
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

// ForceRefresh attempts an immediate internal state refresh
func (ms *ManagedService) ForceRefresh() error {
	if err := ms.Err(); err != nil {
		return err
	}
	ch := make(chan error, 1)
	defer close(ch)
	ms.forceRefresh <- ch
	return <-ch
}

func (ms *ManagedService) findHealth(ctx context.Context, name string, tags []string) (*api.ServiceEntry, *api.QueryMeta, error) {
	var (
		svcs []*api.ServiceEntry
		qm   *api.QueryMeta
		err  error
	)
	if svcs, qm, err = ms.client.Health().ServiceMultipleTags(name, tags, false, ms.qo.WithContext(ctx)); err != nil {
		return nil, qm, fmt.Errorf("error fetching service health: %s", err)
	}
	for _, svc := range svcs {
		if svc.Service.ID == ms.serviceID {
			return svc, qm, nil
		}
	}
	return nil, qm, fmt.Errorf("no service with name %q with id %q found", name, ms.serviceID)
}

// fetchChecks must be used behind a lock
func (ms *ManagedService) fetchChecks(ctx context.Context) (api.HealthChecks, *api.QueryMeta, error) {
	var (
		allChecks api.HealthChecks
		checks    api.HealthChecks
		qm        *api.QueryMeta
		err       error
	)
	if allChecks, qm, err = ms.client.Health().Checks(ms.svc.Service, ms.qo.WithContext(ctx)); err != nil {
		return checks, qm, nil
	}
	checks = make(api.HealthChecks, 0)
	for _, check := range allChecks {
		if check.ServiceID == ms.serviceID {
			checks = append(checks, check)
		}
	}
	return checks, qm, nil
}

// refreshService will refresh the internal representation of the service.
//
// caller must hold lock
func (ms *ManagedService) refreshService(ctx context.Context) (*api.QueryMeta, error) {
	var (
		svc *api.AgentService
		qm  *api.QueryMeta
		err error
	)
	if svc, qm, err = ms.AgentService(ctx); err != nil {
		return qm, err
	}
	ms.svc = svc
	ms.localRefreshed = time.Now()
	return qm, nil
}

func (ms *ManagedService) init() error {
	var (
		//token, datacenter string
		err error
	)
	ms.mu.Lock()
	defer ms.mu.Unlock()

	ctx, cancel := context.WithTimeout(ms.ctx, 2*time.Second)
	defer cancel()
	if _, err = ms.refreshService(ctx); err != nil {
		return fmt.Errorf("error fetching current state of service: %s", err)
	}

	ms.tagOverride = ms.svc.EnableTagOverride

	//if ms.qo != nil {
	//	token = ms.qo.Token
	//	datacenter = ms.qo.Datacenter
	//}

	//if ms.wp, err = WatchServiceMultipleTags(ms.svc.Service, ms.svc.Tags, false, true, token, datacenter); err != nil {
	//	return fmt.Errorf("error creating watch plan: %s", err)
	//}

	return nil
}

// registerService will attempt to re-push the service to the consul agent
//
// caller must hold lock
func (ms *ManagedService) registerService() error {
	reg := new(api.AgentServiceRegistration)
	reg.Kind = ms.svc.Kind
	reg.ID = ms.svc.ID
	reg.Name = ms.svc.Service
	reg.Tags = ms.svc.Tags
	reg.Port = ms.svc.Port
	reg.Address = ms.svc.Address
	reg.TaggedAddresses = ms.svc.TaggedAddresses
	reg.EnableTagOverride = ms.svc.EnableTagOverride
	reg.Meta = ms.svc.Meta
	reg.Weights = &ms.svc.Weights
	reg.Checks = ms.baseChecks
	reg.Proxy = ms.svc.Proxy
	reg.Connect = ms.svc.Connect

	return ms.client.Agent().ServiceRegister(reg)
}

func (ms *ManagedService) maintain() {
	var (
		//wpUpdate     = make(chan []*api.ServiceEntry, 1)
		refreshTimer = time.NewTimer(ms.refreshInterval)
	)

	defer func() {
		// always cancel...
		ms.cancel()
		refreshTimer.Stop()
		//ms.wp.Stop()
		close(ms.forceRefresh)
		ms.done <- ms.client.Agent().ServiceDeregister(ms.serviceID)
		close(ms.done)
	}()

	for {
		select {
		case ch := <-ms.forceRefresh:
			refreshTimer.Stop()

			ms.mu.Lock()
			ctx, cancel := context.WithTimeout(ms.ctx, 2*time.Second)
			_, err := ms.refreshService(ctx)
			cancel()
			ms.mu.Unlock()

			ch <- err

			refreshTimer = time.NewTimer(ms.refreshInterval)

		//case svcs := <-wpUpdate:
		//	if len(svcs) == 0 {
		//		// TODO: handle empty here...
		//	}

		// TODO: finish updated on watch

		case <-refreshTimer.C:
			ms.mu.Lock()
			ctx, cancel := context.WithTimeout(ms.ctx, 2*time.Second)
			// TODO: yell about errors here...
			_, err := ms.refreshService(ctx)
			cancel()
			if err != nil {
				if err := ms.registerService(); err != nil {
					// TODO: inform the implementor
					ms.mu.Unlock()
					return
				}
			}
			ms.mu.Unlock()

			refreshTimer = time.NewTimer(ms.refreshInterval)

		case <-ms.ctx.Done():
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
	if b.Tags == nil {
		b.Tags = make([]string, 0)
	}
	if b.Checks == nil {
		b.Checks = make(api.AgentServiceChecks, 0)
	}
	if b.TaggedAddresses == nil {
		b.TaggedAddresses = make(map[string]api.ServiceAddress)
	}
	if b.Meta == nil {
		b.Meta = make(map[string]string)
	}
	for _, fn := range fns {
		fn(b)
	}
	return b
}

// NewBareManagedServiceBuilder constructs a new builder with the name, address, and port fields defined.  The Address
// value is inferred using the LocalAddress() method and may be overwritten at any time.
func NewBareManagedServiceBuilder(name string, port int, fns ...ManagedServiceBuilderMutator) *ManagedServiceBuilder {
	reg := new(api.AgentServiceRegistration)
	reg.Name = name
	reg.Port = port
	reg.Address, _ = LocalAddress()
	return NewManagedServiceBuilder(reg, fns...)
}

// UseRandomID sets the service's ID to a randomly generated string of at least 12 characters in length
func (b *ManagedServiceBuilder) UseRandomID(n int) *ManagedServiceBuilder {
	if n <= 0 {
		n = 12
	}
	b.ID = LazyRandomString(n)
	return b
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
	for _, fn := range fns {
		fn(check)
	}
	b.Checks = append(b.Checks, check)
	return b
}

// AddHTTPCheck compiles and adds a new HTTP service check based upon the address and port set for the service
func (b *ManagedServiceBuilder) AddHTTPCheck(method, scheme, path string, interval time.Duration, fns ...ManagedServiceCheckMutator) *ManagedServiceBuilder {
	check := new(api.AgentServiceCheck)
	check.HTTP = fmt.Sprintf("%s://%s:%d/%s", strings.TrimRight(scheme, ":/"), b.Address, b.Port, strings.TrimLeft(path, "/"))
	check.Method = method
	check.Interval = interval.String()
	return b.AddCheck(check, fns...)
}

// AddTCPCheck compiles and adds a new TCP service check based upon the address and port set for the service.
func (b *ManagedServiceBuilder) AddTCPCheck(interval time.Duration, fns ...ManagedServiceCheckMutator) *ManagedServiceBuilder {
	check := new(api.AgentServiceCheck)
	check.TCP = fmt.Sprintf("%s:%d", b.Address, b.Port)
	check.Interval = interval.String()
	return b.AddCheck(check, fns...)
}

// AddTTLCheck compiles and adds a new TTL service check.
func (b *ManagedServiceBuilder) AddTTLCheck(originalStatus string, ttl time.Duration, fns ...ManagedServiceCheckMutator) *ManagedServiceBuilder {
	check := new(api.AgentServiceCheck)
	check.TTL = ttl.String()
	check.Status = originalStatus
	return b.AddCheck(check, fns...)
}

// AddScriptCheck compiles and adds a new script service check.
func (b *ManagedServiceBuilder) AddScriptCheck(args []string, interval time.Duration, fns ...ManagedServiceCheckMutator) *ManagedServiceBuilder {
	check := new(api.AgentServiceCheck)
	check.Args = args
	check.Interval = interval.String()
	return b.AddCheck(check, fns...)
}

// AddDockerCheck compiles and adds a new Docker container service check.
func (b *ManagedServiceBuilder) AddDockerCheck(containerID string, shell string, args []string, interval time.Duration, fns ...ManagedServiceCheckMutator) *ManagedServiceBuilder {
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
	check := new(api.AgentServiceCheck)
	check.GRPC = fmt.Sprintf("%s:%d", b.Address, b.Port)
	check.Interval = interval.String()
	return b.AddCheck(check, fns...)
}

// AddAliasCheck compiles and adds an alias service check.
func (b *ManagedServiceBuilder) AddAliasCheck(service, node string, interval time.Duration, fns ...ManagedServiceCheckMutator) *ManagedServiceBuilder {
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
func (b *ManagedServiceBuilder) Build(ctx context.Context, cfg *ManagedServiceConfig) (*ManagedService, error) {
	var err error

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

	if cfg.Client == nil {
		if cfg.Client, err = api.NewClient(api.DefaultConfig()); err != nil {
			return nil, fmt.Errorf("error creating default client: %s", err)
		}
	}

	if err = cfg.Client.Agent().ServiceRegister(b.AgentServiceRegistration); err != nil {
		return nil, fmt.Errorf("error registering service: %s", err)
	}

	return NewManagedService(ctx, cfg)
}
