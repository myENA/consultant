package consultant

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/myENA/go-helpers"
)

type ManagedSessionState uint8

const (
	ManagedSessionStateStopped ManagedSessionState = iota
	ManagedSessionStateRunning
)

func (s ManagedSessionState) String() string {
	switch s {
	case ManagedSessionStateStopped:
		return "stopped"
	case ManagedSessionStateRunning:
		return "running"

	default:
		return "UNKNOWN"
	}
}

// ManagedSessionUpdate is the value of .Data in all Notification types pushed by a ManagedSession instance
type ManagedSessionUpdate struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	LastRenewed int64               `json:"last_renewed"`
	Error       error               `json:"error"`
	State       ManagedSessionState `json:"state"`
}

const (
	SessionMinimumTTL = 10 * time.Second
	SessionDefaultTTL = 30 * time.Second
	SessionMaximumTTL = 24 * time.Hour

	// SessionDefaultNameFormat will be used to create a name for any created ManagedSession instance that did not have
	// one set in its configured definition
	SessionDefaultNameFormat = "managed_session_" + SlugNode + "_" + SlugRand

	// SessionDefaultNodeName is only ever used when:
	// 1. the connected consul node name could not be determined
	// 2. os.Hostname() results in an error
	SessionDefaultNodeName = "LOCAL"
)

var (
	validSessionBehaviors = []string{api.SessionBehaviorDelete, api.SessionBehaviorRelease}
)

// ManagedSessionConfig describes a ManagedSession
type ManagedSessionConfig struct {
	// Definition [suggested]
	//
	// This is the base definition of the session you wish to have managed by a ManagedSession instance.  It must
	// contain all necessary values to build and re-build the session as you see fit, as all values other than ID will
	// be used per session create attempt.
	//
	// If left blank, default values will be used for Name and TTL
	Definition *api.SessionEntry

	// StartImmediately [optional]
	//
	// If provided, the session will be immediately ran with this context
	StartImmediately context.Context

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
	// Optionally specify a logger to use.  No logging will take place if left empty
	Logger Logger

	// Debug [optional]
	//
	// Enables debug-level logging
	Debug bool

	// Client [optional]
	//
	// API client to use for managing this session.  If left empty, a new one will be created using api.DefaultConfig()
	Client *api.Client
}

// ManagedSession
type ManagedSession struct {
	*notifierBase
	mu sync.RWMutex

	client     *api.Client
	qo         *api.QueryOptions
	wo         *api.WriteOptions
	def        *api.SessionEntry
	requestTTL time.Duration

	nf string

	id            string
	ttl           time.Duration
	renewInterval time.Duration
	lastRenewed   time.Time
	lastErr       error

	stop  chan chan error
	state ManagedSessionState

	logger Logger
	dbg    bool
}

// NewManagedSession attempts to create a managed session instance for your immediate use.
func NewManagedSession(conf *ManagedSessionConfig) (*ManagedSession, error) {
	var (
		err error

		ms = new(ManagedSession)
	)

	if conf == nil {
		conf = new(ManagedSessionConfig)
	}

	ms.notifierBase = newNotifierBase()
	ms.dbg = conf.Debug
	ms.logger = conf.Logger
	ms.stop = make(chan chan error, 1)
	ms.qo = conf.QueryOptions
	ms.wo = conf.WriteOptions
	ms.def = new(api.SessionEntry)

	if conf.Definition != nil {
		*ms.def = *conf.Definition
		if conf.Definition.Checks != nil {
			l := len(conf.Definition.Checks)
			ms.def.Checks = make([]string, l, l)
			if l > 0 {
				copy(ms.def.Checks, conf.Definition.Checks)
			}
		}
	}

	if ms.def.TTL == "" {
		ms.def.TTL = SessionDefaultTTL.String()
	}
	if ms.def.Behavior == "" {
		ms.def.Behavior = api.SessionBehaviorDelete
	}
	if conf.RequestTTL > 0 {
		ms.requestTTL = conf.RequestTTL
	} else {
		ms.requestTTL = defaultInternalRequestTTL
	}

	ms.ttl, err = time.ParseDuration(ms.def.TTL)
	if err != nil {
		return nil, fmt.Errorf("provided TTL of %q is not valid: %s", ms.def.TTL, err)
	}
	if ms.ttl < SessionMinimumTTL {
		ms.ttl = SessionMinimumTTL
	} else if ms.ttl > SessionMaximumTTL {
		ms.ttl = SessionMaximumTTL
	}
	ms.def.TTL = ms.ttl.String()

	ms.renewInterval = ms.ttl / 2

	switch ms.def.Behavior {
	case api.SessionBehaviorDelete, api.SessionBehaviorRelease:
	default:
		return nil, fmt.Errorf("behavior must be one of %v, saw %q", validSessionBehaviors, ms.def.Behavior)
	}

	if conf.Client != nil {
		ms.client = conf.Client
	} else if ms.client, err = api.NewClient(api.DefaultConfig()); err != nil {
		return nil, fmt.Errorf("no client provided and error when creating with default config: %s", err)
	}

	if ms.def.Node == "" {
		if ms.def.Node, err = ms.client.Agent().NodeName(); err != nil {
			ms.logf(false, "node name not set and unable to determine name of local agent node: %s", err)
		}
	}

	if ms.def.Name == "" {
		ms.def.Name = buildDefaultSessionName(ms.def)
	}

	if conf.StartImmediately != nil {
		ms.logf(true, "StartImmediately enabled")
		if err := ms.Run(conf.StartImmediately); err != nil {
			return nil, err
		}
	}

	ms.logf(true, "Lock timeout: %s", ms.def.TTL)
	ms.logf(true, "Renew interval: %s", ms.renewInterval)

	return ms, nil
}

// ID returns the is of the session as it exists in consul.  This value will be empty until the session has been
// initialized, and then may become empty later if the session is removed.
func (ms *ManagedSession) ID() string {
	ms.mu.RLock()
	sid := ms.id
	ms.mu.RUnlock()
	return sid
}

// Name returns the name of the session as it exists in consul.  This value will be empty until the session has been
// initialized, and then may become empty later if the session is removed.
func (ms *ManagedSession) Name() string {
	ms.mu.RLock()
	name := ms.def.Name
	ms.mu.RUnlock()
	return name
}

// TTL is the timeout limit for this session before which a refresh must happen, or the configured TTLBehavior will take
// place
func (ms *ManagedSession) TTL() time.Duration {
	return ms.ttl
}

// TTLBehavior is the action that will take place if the TTL is allowed to expire
func (ms *ManagedSession) TTLBehavior() string {
	return ms.def.Behavior
}

// RenewInterval is the renewInterval at which a TTL reset will be attempted
func (ms *ManagedSession) RenewInterval() time.Duration {
	return ms.renewInterval
}

// LastRenewed returns the last point at which the TTL was successfully reset
func (ms *ManagedSession) LastRenewed() time.Time {
	ms.mu.RLock()
	t := ms.lastRenewed
	ms.mu.RUnlock()
	return t
}

// SessionEntry attempts to immediately pull the latest state of the upstream session from Consul
func (ms *ManagedSession) SessionEntry(ctx context.Context) (*api.SessionEntry, *api.QueryMeta, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	if ms.state != ManagedSessionStateRunning {
		return nil, nil, errors.New("session is not running")
	}
	if ms.id == "" {
		return nil, nil, errors.New("session is not currently defined")
	}
	return ms.client.Session().Info(ms.id, ms.qo.WithContext(ctx))
}

// PushStateNotification will immediate push the current managed session state to all attached notification recipients
func (ms *ManagedSession) PushStateNotification() {
	ms.mu.RLock()
	ms.pushNotification(NotificationEventManualPush, ms.buildUpdate())
	ms.mu.RUnlock()
}

// Running returns true so long as the internal session state is active
func (ms *ManagedSession) Running() bool {
	ms.mu.RLock()
	b := ms.state == ManagedSessionStateRunning
	ms.mu.RUnlock()
	return b
}

// Run immediately starts attempting to acquire a session lock on the configured kv key
func (ms *ManagedSession) Run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ms.mu.Lock()
	if ms.state == ManagedSessionStateRunning {
		// if our state is already running, just continue to do so.
		ms.logf(true, "Run() called but I'm already running")
		ms.mu.Unlock()
		return nil
	}

	ms.mu.Unlock()

	// modify state
	ms.setState(ManagedSessionStateRunning)

	// try to create session immediately
	ms.create(ctx)
	if ms.lastErr != nil {
		ms.logf(
			false,
			"Unable to perform initial session creation, will try again in \"%d\" seconds: %s",
			int64(ms.renewInterval.Seconds()),
			ms.lastErr)
	} else {
		ms.logf(true, "Run() - New upstream session created: %s", ms.id)
	}

	// release lock before beginning maintenance loop

	go ms.maintain(ctx)

	return nil
}

// Stop immediately attempts to cease session management
func (ms *ManagedSession) Stop() error {
	ms.mu.Lock()
	if ms.state == ManagedSessionStateStopped {
		ms.logf(true, "Stop() called but I'm already stopped")
		ms.mu.Unlock()
		return nil
	}
	ms.mu.Unlock()

	ms.setState(ManagedSessionStateStopped)

	stopped := make(chan error, 1)
	ms.stop <- stopped
	err := <-stopped
	close(stopped)

	return err
}

// State returns the current running state of the managed session
func (ms *ManagedSession) State() ManagedSessionState {
	ms.mu.RLock()
	s := ms.state
	ms.mu.RUnlock()
	return s
}

func (ms *ManagedSession) LastError() error {
	ms.mu.RLock()
	err := ms.lastErr
	ms.mu.RUnlock()
	return err
}

func (ms *ManagedSession) logf(debug bool, f string, v ...interface{}) {
	if ms.logger == nil || debug && !ms.dbg {
		return
	}
	ms.logger.Printf(f, v...)
}

// buildUpdate constructs an update obj to be provided to pushNotification
//
// Caller must hold lock
func (ms *ManagedSession) buildUpdate() ManagedSessionUpdate {
	return ManagedSessionUpdate{
		ID:          ms.id,
		Name:        ms.def.Name,
		LastRenewed: ms.lastRenewed.UnixNano(),
		Error:       ms.lastErr,
		State:       ms.state,
	}
}

// pushNotification constructs and then pushes a new notification to currently registered recipients based on the
// current state of the session.
func (ms *ManagedSession) pushNotification(ev NotificationEvent, up ManagedSessionUpdate) {
	ms.sendNotification(NotificationSourceManagedSession, ev, up)
}

// setState updates the internal state value and pushes a notification of change
//
// caller must hold full lock
func (ms *ManagedSession) setState(state ManagedSessionState) {
	ms.mu.Lock()

	var ev NotificationEvent

	switch state {
	case ManagedSessionStateRunning:
		ev = NotificationEventManagedSessionRunning
	case ManagedSessionStateStopped:
		ev = NotificationEventManagedSessionStopped

	default:
		panic(fmt.Sprintf("unknown state %d (%[1]s) seen", state))
	}

	ms.state = state

	up := ms.buildUpdate()

	ms.mu.Unlock()

	ms.pushNotification(ev, up)
}

// create will attempt to do just that.
//
// caller must hold lock
func (ms *ManagedSession) create(ctx context.Context) {
	ms.mu.Lock()

	ms.logf(true, "create() - Attempting to create upstream session...")

	se := *ms.def

	ctx, cancel := context.WithTimeout(ctx, ms.requestTTL)
	defer cancel()
	ms.id, _, ms.lastErr = ms.client.Session().Create(&se, ms.wo.WithContext(ctx))

	if ms.lastErr == nil {
		ms.lastRenewed = time.Now()
		ms.logf(true, "create() - Upstream session created: %s", ms.id)
	} else {
		ms.logf(false, "create() - Error creating upstream session: %s", ms.lastErr)
	}

	up := ms.buildUpdate()

	ms.mu.Unlock()

	ms.pushNotification(NotificationEventManagedSessionCreate, up)
}

// renew will attempt to do just that.
//
// caller must hold lock
func (ms *ManagedSession) renew(ctx context.Context) {
	ms.mu.Lock()

	if ms.id == "" {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, ms.requestTTL)
	defer cancel()
	se, _, err := ms.client.Session().Renew(ms.id, ms.wo.WithContext(ctx))
	if err != nil {
		ms.logf(false, "renew() - Error refreshing upstream session (%s), clearing local references...", err)
		ms.id = ""
		ms.lastErr = err
	} else if se != nil {
		ms.logf(true, "renew() - Upstream session renewed")
		ms.id = se.ID
		ms.lastRenewed = time.Now()
		ms.lastErr = nil
	} else {
		ms.logf(false, "renew() - Upstream session not found, will recreate on next pass")
		ms.id = ""
		err = errors.New("upstream session not found")
		ms.lastErr = err
	}

	up := ms.buildUpdate()

	ms.mu.Unlock()

	ms.pushNotification(NotificationEventManagedSessionRenew, up)
}

// destroy will attempt to destroy the upstream session and removes internal references to it.
//
// caller must hold lock
func (ms *ManagedSession) destroy(ctx context.Context) {
	ms.mu.Lock()

	if ms.id == "" {
		ms.mu.Unlock()
		return
	}

	sid := ms.id
	ctx, cancel := context.WithTimeout(ctx, ms.requestTTL)
	defer cancel()
	_, ms.lastErr = ms.client.Session().Destroy(sid, ms.wo.WithContext(ctx))
	ms.id = ""
	ms.lastRenewed = time.Time{}
	if ms.lastErr != nil {
		ms.logf(false, "destroy() - Error destroying upstream session: %s", ms.lastErr)
	} else {
		ms.logf(true, "destroy() - Upstream session destroyed")
	}

	up := ms.buildUpdate()

	ms.mu.Unlock()

	ms.pushNotification(NotificationEventManagedSessionDestroy, up)
}

// maintainTick is responsible for ensuring our session is kept alive in Consul
func (ms *ManagedSession) maintainTick(ctx context.Context) {
	if ms.id != "" {
		if !ms.lastRenewed.IsZero() && time.Now().Sub(ms.lastRenewed) > ms.ttl {
			// if we have a session but the last time we were able to successfully renew it was beyond the TTL,
			// attempt to destroy and allow re-creation down below
			ms.logf(
				true,
				"maintainTick() - Last renewed time (%s) is > ttl (%s), expiring upstream session %q...",
				ms.lastRenewed.Format(time.RFC822),
				ms.ttl,
				ms.id,
			)
			ms.destroy(ctx)
		} else {
			ms.renew(ctx)
		}
	}

	if ms.id == "" {
		// if this is the first iteration of the loop or if an error occurred above, test and try to create
		// a new session
		ms.create(ctx)
	}
}

// shutdown will clean up the state of the managed session on shutdown.
//
// caller MUST hold lock!
func (ms *ManagedSession) shutdown() {
	ms.logf(false, "shutdown() - Stopping session...")

	if ms.id != "" {
		ctx, cancel := context.WithTimeout(context.Background(), ms.requestTTL)
		defer cancel()
		// if we have a reference to an upstream session id, attempt to destroy it
		ms.destroy(ctx)
	}

	// set our state to stopped, preventing further interaction.
	ms.setState(ManagedSessionStateStopped)

	ms.logf(false, "shutdown() - ManagedSession stopped")
}

// TODO: improve updates to include the action taken this loop, and whether it is the last action to be taken this loop
// i.e., destroy / renew can happen in the same loop as create.
func (ms *ManagedSession) maintain(ctx context.Context) {
	var (
		tick time.Time
		drop chan error

		intervalTimer = time.NewTimer(ms.renewInterval)
	)

	go func() {
		<-ctx.Done()
		ms.logf(false, "maintainLock() - running context completed with: %s", ctx.Err())
		if err := ms.Stop(); err != nil {
			ms.logf(false, "maintainLock() - Error stopping session: %s", err)
		}
	}()

	defer func() {
		intervalTimer.Stop()
		ms.shutdown()
		if drop != nil {
			drop <- ms.LastError()
		}
	}()

	for {
		select {
		case tick = <-intervalTimer.C:
			ms.logf(true, "maintainLock() - intervalTimer hit (%s)", tick)
			ms.maintainTick(ctx)
			intervalTimer.Reset(ms.renewInterval)

		case drop = <-ms.stop:
			ms.logf(false, "maintainLock() - explicit stop called")
			return
		}
	}
}

// ManagedSessionEntry is a thin wrapper that provides guided construction to a SessionEntry, resulting in a
// ManagedSession instance once built
type ManagedSessionEntry struct {
	api.SessionEntry
}

// ManagedSessionEntryMutator defines a callback that may mutate a new MangedSessionBuilder instance
type ManagedSessionEntryMutator func(*ManagedSessionEntry)

// NewManagedSessionEntry creates a new
func NewManagedSessionEntry(base *api.SessionEntry, fns ...ManagedSessionEntryMutator) *ManagedSessionEntry {
	b := new(ManagedSessionEntry)
	if base == nil {
		base = new(api.SessionEntry)
	}
	b.SessionEntry = *base
	for _, fn := range fns {
		fn(b)
	}
	return b
}

// SetTTL allows setting of the session TTL from an existing time.Duration instance
func (b *ManagedSessionEntry) SetTTL(ttl time.Duration) *ManagedSessionEntry {
	b.TTL = ttl.String()
	return b
}

// AddCheckNames adds the provided list of check name(s) to final session entry, ensuring uniqueness of input
func (b *ManagedSessionEntry) AddCheckNames(checkNames ...string) *ManagedSessionEntry {
	if b.Checks == nil {
		b.Checks = make([]string, 0)
	}
	b.Checks = helpers.UniqueStringSlice(append(b.Checks, checkNames...))
	return b
}

// SetName sets the name of the to be created session, optionally allowing for replacing
func (b *ManagedSessionEntry) SetName(f string, v ...interface{}) *ManagedSessionEntry {
	b.Name = ReplaceSlugs(fmt.Sprintf(f, v...), SlugParams{Node: b.Node})
	return b
}

// Create returns a new ManagedSession instance based upon this ManagedSessionEntry
func (b *ManagedSessionEntry) Create(cfg *ManagedSessionConfig) (*ManagedSession, error) {
	var act = new(ManagedSessionConfig)

	if cfg == nil {
		cfg = new(ManagedSessionConfig)
	}

	*act = *cfg

	act.Definition = &b.SessionEntry

	return NewManagedSession(act)
}
