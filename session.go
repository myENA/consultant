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

type SessionState uint8

const (
	SessionStateStopped SessionState = iota
	SessionStateRunning
)

func (s SessionState) String() string {
	switch s {
	case SessionStateStopped:
		return "stopped"
	case SessionStateRunning:
		return "running"

	default:
		return "UNKNOWN"
	}
}

const (
	SessionDefaultTTL        = 30 * time.Second
	SessionDefaultNameFormat = "managed_session_" + SlugNode + "_" + SlugRand
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
	mu sync.RWMutex

	client *api.Client
	qo     *api.QueryOptions
	wo     *api.WriteOptions
	base   *api.SessionEntry
	rttl   time.Duration

	nf string

	id            string
	ttl           time.Duration
	renewInterval time.Duration
	lastRenewed   time.Time
	lastErr       error

	stop  chan chan error
	state SessionState

	watchers *managedSessionWatchers

	logger Logger
	dbg    bool
}

// NewManagedSession attempts to create a managed session instance for your immediate use.
func NewManagedSession(conf *ManagedSessionConfig) (*ManagedSession, error) {
	var (
		err error

		ms = new(ManagedSession)
	)

	ms.dbg = conf.Debug
	ms.logger = conf.Logger
	ms.stop = make(chan chan error, 1)
	ms.watchers = newManagedSessionWatchers()
	ms.qo = conf.QueryOptions
	ms.wo = conf.WriteOptions
	ms.base = new(api.SessionEntry)

	if conf.Definition != nil {
		*ms.base = *conf.Definition
		if l := len(conf.Definition.Checks); l > 0 {
			ms.base.Checks = make([]string, l, l)
			copy(ms.base.Checks, conf.Definition.Checks)
		}
	} else {
		ms.base.TTL = SessionDefaultTTL.String()
		ms.base.Behavior = api.SessionBehaviorDelete
	}

	if conf.RequestTTL > 0 {
		ms.rttl = conf.RequestTTL
	} else {
		ms.rttl = defaultInternalRequestTTL
	}

	ms.ttl, err = time.ParseDuration(ms.base.TTL)
	if err != nil {
		return nil, fmt.Errorf("provided TTL of %q is not valid: %s", ms.base.TTL, err)
	}
	if ms.ttl < 10*time.Second {
		ms.ttl = 10 * time.Second
	} else if ms.ttl > 86400*time.Second {
		ms.ttl = 86400 * time.Second
	}
	ms.base.TTL = ms.ttl.String()

	ms.renewInterval = ms.ttl / 2

	switch ms.base.Behavior {
	case api.SessionBehaviorDelete, api.SessionBehaviorRelease:
	default:
		return nil, fmt.Errorf("ttlBehavior must be one of %v, saw %q", []string{api.SessionBehaviorRelease, api.SessionBehaviorDelete}, ms.base.Behavior)
	}

	if conf.Client != nil {
		ms.client = conf.Client
	} else if ms.client, err = api.NewClient(api.DefaultConfig()); err != nil {
		return nil, fmt.Errorf("no client provided and error when creating with default config: %s", err)
	}

	if ms.base.Node == "" {
		if ms.base.Node, err = ms.client.Agent().NodeName(); err != nil {
			return nil, fmt.Errorf("node name not set and unable to determine name of local agent node: %s", err)
		}
	}

	if ms.base.Name == "" {
		ms.base.Name = ReplaceSlugs(SessionDefaultNameFormat, SlugParams{Node: ms.base.Node})
	}

	if conf.StartImmediately != nil {
		ms.logf(true, "StartImmediately enabled")
		if err := ms.Run(conf.StartImmediately); err != nil {
			return nil, err
		}
	}

	ms.logf(true, "Lock timeout: %s", ms.base.TTL)
	ms.logf(true, "Renew renewInterval: %s", ms.renewInterval)

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
	name := ms.base.Name
	ms.mu.RUnlock()
	return name
}

// TTL is the timeout limit for this session before which a refresh must happen, or the configured TTLBehavior will take
// place
func (ms *ManagedSession) TTL() time.Duration {
	return ms.ttl
}

// TTLBehavior is the action that will take place if the TTL is allowed to expire
func (ms *ManagedSession) Behavior() string {
	return ms.base.Behavior
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
	if ms.state != SessionStateRunning {
		return nil, nil, errors.New("session is not running")
	}
	if ms.id == "" {
		return nil, nil, errors.New("session is not currently defined")
	}
	return ms.client.Session().Info(ms.id, ms.qo.WithContext(ctx))
}

// Watch allows you to register a function that will be called when the election SessionState has changed
func (ms *ManagedSession) Watch(id string, fn ManagedSessionWatchFunc) string {
	return ms.watchers.Add(id, fn)
}

// Unwatch will remove a function from the list of watchers.
func (ms *ManagedSession) Unwatch(id string) {
	ms.watchers.Remove(id)
}

// RemoveWatchers will clear all watchers
func (ms *ManagedSession) RemoveWatchers() {
	ms.watchers.RemoveAll()
}

// UpdateWatchers will immediately push the current state of this Candidate to all currently registered Watchers
func (ms *ManagedSession) UpdateWatchers() {
	ms.mu.RLock()
	ms.watchers.notify(ManagedSessionUpdate{ms.id, ms.base.Name, ms.lastRenewed, ms.lastErr, ms.state})
	ms.mu.RUnlock()
}

// Running returns true so long as the internal session state is active
func (ms *ManagedSession) Running() bool {
	ms.mu.RLock()
	b := ms.state == SessionStateRunning
	ms.mu.RUnlock()
	return b
}

// Run immediately starts attempting to acquire a session lock on the configured kv key
func (ms *ManagedSession) Run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ms.mu.Lock()
	if ms.state == SessionStateRunning {
		// if our state is already running, just continue to do so.
		ms.logf(true, "Run() called but I'm already running")
		ms.mu.Unlock()
		return nil
	}

	// modify state
	ms.state = SessionStateRunning

	// try to create session immediately
	if err := ms.create(ctx); err != nil {
		ms.logf(
			false,
			"Unable to perform initial session creation, will try again in \"%d\" seconds: %s",
			int64(ms.renewInterval.Seconds()),
			err)
	}

	// release lock before beginning maintenance loop
	ms.mu.Unlock()

	go ms.maintain(ctx)

	return nil
}

// Stop immediately attempts to cease session management
func (ms *ManagedSession) Stop() error {
	ms.mu.Lock()
	if ms.state == SessionStateStopped {
		ms.logf(true, "Stop() called but I'm already stopped")
		ms.mu.Unlock()
		return nil
	}
	ms.state = SessionStateStopped
	ms.mu.Unlock()

	stopped := make(chan error, 1)
	ms.stop <- stopped
	err := <-stopped
	close(stopped)

	return err
}

// State returns the current running state of the managed session
func (ms *ManagedSession) State() SessionState {
	ms.mu.RLock()
	s := ms.state
	ms.mu.RUnlock()
	return s
}

func (ms *ManagedSession) logf(debug bool, f string, v ...interface{}) {
	if ms.logger == nil || debug && !ms.dbg {
		return
	}
	ms.logger.Printf(f, v...)
}

// create will attempt to do just that.
//
// caller must hold lock
func (ms *ManagedSession) create(ctx context.Context) error {
	ms.logf(true, "create() - Attempting to create upstream session...")

	se := *ms.base

	ctx, cancel := context.WithTimeout(ctx, ms.rttl)
	defer cancel()
	sid, _, err := ms.client.Session().Create(&se, ms.wo.WithContext(ctx))
	if err != nil {
		ms.id = ""
		ms.lastErr = err
	} else if sid != "" {
		ms.logf(true, "create() - New upstream session %q created", sid)
		ms.id = sid
		ms.lastRenewed = time.Now()
		ms.lastErr = nil
	} else {
		ms.id = ""
		err = errors.New("internal error creating session")
		ms.lastErr = err
	}

	return err
}

// renew will attempt to do just that.
//
// caller must hold lock
func (ms *ManagedSession) renew(ctx context.Context) error {
	if ms.id == "" {
		ms.logf(true, "renew() - session cannot be renewed as it doesn't exist yet")
		return errors.New("session does not exist yet")
	}

	ctx, cancel := context.WithTimeout(ctx, ms.rttl)
	defer cancel()
	se, _, err := ms.client.Session().Renew(ms.id, ms.wo.WithContext(ctx))
	if err != nil {
		ms.id = ""
		ms.lastErr = err
	} else if se != nil {
		ms.logf(true, "renew() - Upstream session %q renewed", se.ID)
		ms.id = se.ID
		ms.lastRenewed = time.Now()
		ms.lastErr = nil
	} else {
		ms.id = ""
		err = errors.New("internal error renewing session")
		ms.lastErr = err
	}

	return err
}

// destroy will attempt to destroy the upstream session and removes internal references to it.
//
// caller must hold lock
func (ms *ManagedSession) destroy(ctx context.Context) error {
	sid := ms.id
	ctx, cancel := context.WithTimeout(ctx, ms.rttl)
	defer cancel()
	_, err := ms.client.Session().Destroy(sid, ms.wo.WithContext(ctx))
	if err != nil {
		ms.logf(true, "destroy() - Upstream session %q destroyed", sid)
	}
	ms.id = ""
	ms.lastRenewed = time.Time{}
	ms.lastErr = err
	return err
}

// maintainTick is responsible for ensuring our session is kept alive in Consul
//
// caller must hold lock
func (ms *ManagedSession) maintainTick(ctx context.Context) {
	var (
		sid, name string
		err       error
	)

	if ms.id != "" {
		// if we were previously able to create an upstream session...
		sid = ms.id
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
			if err = ms.destroy(ctx); err != nil {
				ms.logf(
					false,
					"maintainTick() - Error destroying expired upstream session %q (%q). This can probably be ignored: %s",
					name,
					sid,
					err,
				)
			}
		} else if err = ms.renew(ctx); err != nil {
			// if error during renewal
			ms.logf(false, "maintainTick() - Unable to renew Consul ManagedSession: %s", err)
			// TODO: possibly attempt to destroy the session at this point?  the above timeout test statement
			// should eventually be hit if this continues to fail...
		} else {
			// session should be in a happy state.
			ms.logf(true, "maintainTick() - Upstream session %q renewed", ms.id)
		}
	}

	if ms.id == "" {
		// if this is the first iteration of the loop or if an error occurred above, test and try to create
		// a new session
		if err = ms.create(ctx); err != nil {
			ms.logf(false, "maintainTick() - Unable to create upstream session: %s", err)
		} else {
			ms.logf(true, "maintainTick() - New upstream session %q created.", ms.id)
		}
	}
}

func (ms *ManagedSession) shutdown(intervalTimer *time.Timer, stopped chan<- error) {
	var (
		sid string
		err error
	)

	ms.mu.Lock()

	if !intervalTimer.Stop() {
		<-intervalTimer.C
	}

	ms.logf(false, "shutdown() - Stopping session...")

	// localize most recent upstream session info
	sid = ms.id

	if ms.id != "" {
		// if we have a reference to an upstream session id, attempt to destroy it.
		// use background context here to ensure attempt is made
		if err = ms.destroy(context.Background()); err != nil {
			ms.logf(false, "shutdown() - Error destroying upstream session %q: %s", sid, err)
			// if there was an existing error, append this error to it to be sent along the Stop() resp chan
			err = fmt.Errorf("error destroying session %q during shutdown: %s", sid, err)
		} else {
			ms.logf(true, "shutdown() - Upstream session %q destroyed", sid)
		}
	}

	// set our state to stopped, preventing further interaction.
	ms.state = SessionStateStopped

	ms.lastErr = err

	// if this session was stopped via context, this will be nil
	if stopped != nil {
		// send along the last seen error, whatever it was.
		stopped <- err
	}

	ms.mu.Unlock()

	ms.UpdateWatchers()

	ms.logf(false, "shutdown() - ManagedSession stopped")
}

// TODO: improve updates to include the action taken this loop, and whether it is the last action to be taken this loop
// i.e., destroy / renew can happen in the same loop as create.
func (ms *ManagedSession) maintain(ctx context.Context) {
	var (
		tick    time.Time
		stopped chan error

		intervalTimer = time.NewTimer(ms.renewInterval)
	)

	defer ms.shutdown(intervalTimer, stopped)

	for {
		select {
		case tick = <-intervalTimer.C:
			ms.logf(true, "maintain() - intervalTimer hit: %s", tick)
			ms.mu.Lock()
			ms.maintainTick(ctx)
			ms.mu.Unlock()
			ms.UpdateWatchers()
			intervalTimer = time.NewTimer(ms.renewInterval)

		case <-ctx.Done():
			ms.logf(false, "maintain() - running context completed with: %s", ctx.Err())
			return

		case stopped = <-ms.stop:
			ms.logf(false, "maintain() - explicit stop called")
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

// Create attempts to
func (b *ManagedSessionEntry) Create(cfg *ManagedSessionConfig) (*ManagedSession, error) {
	var act = new(ManagedSessionConfig)

	if cfg == nil {
		cfg = new(ManagedSessionConfig)
	}

	*act = *cfg

	act.Definition = &b.SessionEntry

	return NewManagedSession(act)
}

// ManagedSessionUpdate will be sent to any / all watchers of this managed session upon a significant event happening
type ManagedSessionUpdate struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	LastRenewed time.Time    `json:"last_renewed"`
	Error       error        `json:"error"`
	State       SessionState `json:"state"`
}

// ManagedSessionWatchFunc can be defined in your implementation to be called when a significant event happens in the
// lifecycle of a ManagedSession
type ManagedSessionWatchFunc func(update ManagedSessionUpdate)

type managedSessionWatchers struct {
	mu    sync.RWMutex
	funcs map[string]ManagedSessionWatchFunc
}

func newManagedSessionWatchers() *managedSessionWatchers {
	w := &managedSessionWatchers{
		funcs: make(map[string]ManagedSessionWatchFunc),
	}
	return w
}

// Watch allows you to register a function that will be called when the election SessionState has changed
func (c *managedSessionWatchers) Add(id string, fn ManagedSessionWatchFunc) string {
	c.mu.Lock()
	if id == "" {
		id = LazyRandomString(8)
	}
	_, ok := c.funcs[id]
	if !ok {
		c.funcs[id] = fn
	}
	c.mu.Unlock()
	return id
}

// Unwatch will remove a function from the list of managedSessionWatchers.
func (c *managedSessionWatchers) Remove(id string) {
	c.mu.Lock()
	delete(c.funcs, id)
	c.mu.Unlock()
}

func (c *managedSessionWatchers) RemoveAll() {
	c.mu.Lock()
	c.funcs = make(map[string]ManagedSessionWatchFunc)
	c.mu.Unlock()
}

// notifyWatchers is a thread safe update of leader status
func (c *managedSessionWatchers) notify(update ManagedSessionUpdate) {
	c.mu.RLock()
	for _, fn := range c.funcs {
		go fn(update)
	}
	c.mu.RUnlock()
}
