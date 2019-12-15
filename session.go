package consultant

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"

	"github.com/pkg/errors"
)

const (
	SessionStateStopped SessionState = iota
	SessionStateRunning
)

const (
	SessionDefaultTTL = "30s"
)

type (
	SessionState uint8

	SessionNameParts struct {
		Key      string
		NodeName string
		RandomID string
	}

	SessionUpdate struct {
		ID          string
		Name        string
		LastRenewed time.Time
		Error       error
		State       SessionState
	}

	SessionConfig struct {
		// Key [suggested]
		//
		// Implementation-specific Key to be placed in session key.
		Key string

		// TTL [optional]
		//
		// Session TTL, defaults to value of SessionDefaultTTL
		TTL string

		// Behavior [optional]
		//
		// Session timeout behavior, defaults to "release"
		Behavior string

		// Log [optional]
		//
		// Logger for this session.  One will be created if value is empty
		Log *log.Logger

		// Client [optional]
		//
		// Consul API client, default will be created if not provided
		Client *api.Client

		// UpdateFunc [optional]
		//
		// Callback to be executed after session state change
		UpdateFunc SessionWatchFunc

		// AutoRun [optional]
		//
		// Whether the session should start immediately after successful construction
		AutoRun bool
	}

	Session struct {
		mu     sync.RWMutex
		log    *log.Logger
		client *api.Client

		node string
		key  string

		id       string
		name     string
		ttl      time.Duration
		behavior string

		interval    time.Duration
		lastRenewed time.Time

		stop  chan chan error
		state SessionState

		watchers *sessionWatchers
	}
)

func NewSession(conf *SessionConfig) (*Session, error) {
	var (
		key, ttl, behavior string
		client             *api.Client
		l                  *log.Logger
		updateFunc         SessionWatchFunc
		autoRun            bool
		err                error
	)

	if conf != nil {
		key = conf.Key
		ttl = conf.TTL
		behavior = conf.Behavior
		l = conf.Log
		client = conf.Client
		updateFunc = conf.UpdateFunc
		autoRun = conf.AutoRun
	}

	if behavior == "" {
		behavior = api.SessionBehaviorRelease
	} else {
		switch behavior {
		case api.SessionBehaviorDelete, api.SessionBehaviorRelease:
		default:
			return nil, fmt.Errorf("\"%s\" is not a valid session behavior", behavior)
		}
	}

	if client == nil {
		client, err = api.NewClient(api.DefaultConfig())
		if err != nil {
			return nil, fmt.Errorf("no Consul api client provided and unable to create: %s", err)
		}
	}

	if ttl == "" {
		ttl = SessionDefaultTTL
	}

	ttlTD, err := time.ParseDuration(ttl)
	if err != nil {
		return nil, fmt.Errorf("\"%s\" is not valid: %s", ttl, err)
	}

	ttlSeconds := ttlTD.Seconds()
	if ttlSeconds < 10 {
		ttlTD = 10 * time.Second
	} else if ttlSeconds > 86400 {
		ttlTD = 86400 * time.Second
	}

	cs := &Session{
		log:      l,
		client:   client,
		key:      key,
		ttl:      ttlTD,
		behavior: behavior,
		interval: time.Duration(int64(ttlTD) / 2),
		stop:     make(chan chan error, 1),
		watchers: newSessionWatchers(),
	}

	if updateFunc != nil {
		cs.watchers.Add("", updateFunc)
	}

	if cs.node, err = client.Agent().NodeName(); err != nil {
		return nil, fmt.Errorf("unable to determine node: %s", err)
	}

	l.Printf("Lock interval: %d seconds", int64(ttlTD.Seconds()))
	l.Printf("Session renew interval: %d seconds", int64(cs.interval.Seconds()))

	if autoRun {
		l.Printf("AutoRun enabled")
		cs.Run()
	}

	return cs, nil
}

func (cs *Session) ID() string {
	cs.mu.RLock()
	sid := cs.id
	cs.mu.RUnlock()
	return sid
}

func (cs *Session) Name() string {
	cs.mu.RLock()
	name := cs.name
	cs.mu.RUnlock()
	return name
}

func (cs *Session) TTL() time.Duration {
	return cs.ttl
}

func (cs *Session) Key() string {
	return cs.key
}

func (cs *Session) Behavior() string {
	return cs.behavior
}

func (cs *Session) RenewInterval() time.Duration {
	return cs.interval
}

func (cs *Session) LastRenewed() time.Time {
	cs.mu.RLock()
	t := cs.lastRenewed
	cs.mu.RUnlock()
	return t
}

// Watch allows you to register a function that will be called when the election SessionState has changed
func (cs *Session) Watch(id string, fn SessionWatchFunc) string {
	return cs.watchers.Add(id, fn)
}

// Unwatch will remove a function from the list of watchers.
func (cs *Session) Unwatch(id string) {
	cs.watchers.Remove(id)
}

// RemoveWatchers will clear all watchers
func (cs *Session) RemoveWatchers() {
	cs.watchers.RemoveAll()
}

// UpdateWatchers will immediately push the current state of this Candidate to all currently registered Watchers
func (cs *Session) UpdateWatchers() {
	cs.mu.RLock()
	cs.watchers.notify(SessionUpdate{cs.id, cs.name, cs.lastRenewed, nil, cs.state})
	cs.mu.RUnlock()
}

func (cs *Session) Running() bool {
	cs.mu.RLock()
	b := cs.state == SessionStateRunning
	cs.mu.RUnlock()
	return b
}

func (cs *Session) Run() {
	cs.mu.Lock()
	if cs.state == SessionStateRunning {
		// if our state is already running, just continue to do so.
		cs.log.Printf("Run() called but I'm already running")
		cs.mu.Unlock()
		return
	}

	// modify state
	cs.state = SessionStateRunning

	// try to create session immediately
	if err := cs.create(); err != nil {
		cs.log.Printf(
			"Unable to perform initial session creation, will try again in \"%d\" seconds: %s",
			int64(cs.interval.Seconds()),
			err)
	}

	// release lock before beginning maintenance loop
	cs.mu.Unlock()

	go cs.maintain()
}

func (cs *Session) Stop() error {
	cs.mu.Lock()
	if cs.state == SessionStateStopped {
		cs.log.Printf("Stop() called but I'm already stopped")
		cs.mu.Unlock()
		return nil
	}
	cs.state = SessionStateStopped
	cs.mu.Unlock()

	stopped := make(chan error, 1)
	cs.stop <- stopped
	err := <-stopped
	close(stopped)
	return err
}

func (cs *Session) State() SessionState {
	cs.mu.RLock()
	s := cs.state
	cs.mu.RUnlock()
	return s
}

// create will attempt to do just that. Caller MUST hold lock!
func (cs *Session) create() error {
	var name string

	if cs.key == "" {
		name = fmt.Sprintf("%s_%s", cs.node, LazyRandomString(12))
	} else {
		name = fmt.Sprintf("%s_%s_%s", cs.key, cs.node, LazyRandomString(12))
	}

	cs.log.Printf("Attempting to create Consul Session \"%s\"...", name)

	se := &api.SessionEntry{
		Name:     name,
		Behavior: cs.behavior,
		TTL:      cs.ttl.String(),
	}

	sid, _, err := cs.client.Session().Create(se, nil)
	if err != nil {
		cs.id = ""
		cs.name = ""
	} else if sid != "" {
		cs.log.Printf("New upstream session %q created", sid)
		cs.id = sid
		cs.name = name
		cs.lastRenewed = time.Now()
	} else {
		cs.id = ""
		cs.name = ""
		err = errors.New("internal error creating session")
	}

	return err
}

// renew will attempt to do just that.  Caller MUST hold lock!
func (cs *Session) renew() error {
	if cs.id == "" {
		cs.log.Print("Session cannot be renewed as it doesn't exist yet")
		return errors.New("session does not exist yet")
	}

	se, _, err := cs.client.Session().Renew(cs.id, nil)
	if err != nil {
		cs.id = ""
		cs.name = ""
	} else if se != nil {
		cs.id = se.ID
		cs.lastRenewed = time.Now()
	} else {
		cs.id = ""
		cs.name = ""
		err = errors.New("internal error renewing session")
	}

	return err
}

// destroy will attempt to destroy the upstream session and removes internal references to it.
// caller MUST hold lock!
func (cs *Session) destroy() error {
	sid := cs.id
	cs.id = ""
	cs.name = ""
	cs.lastRenewed = time.Time{}
	_, err := cs.client.Session().Destroy(sid, nil)
	return err
}

func (cs *Session) updateWatchers(err error) {
	cs.mu.RLock()
	cs.watchers.notify(SessionUpdate{cs.id, cs.name, cs.lastRenewed, err, cs.state})
	cs.mu.RUnlock()
}

// maintainTick is responsible for ensuring our session is kept alive in Consul
func (cs *Session) maintainTick(tick time.Time) {
	var (
		sid, name string
		err       error
	)

	cs.mu.Lock()

	if cs.id != "" {
		// if we were previously able to create an upstream session...
		sid, name = cs.id, cs.name
		if !cs.lastRenewed.IsZero() && time.Now().Sub(cs.lastRenewed) > cs.ttl {
			// if we have a session but the last time we were able to successfully renew it was beyond the TTL,
			// attempt to destroy and allow re-creation down below
			cs.log.Printf(
				"maintainTick() - Last renewed time (%s) is > ttl (%s), expiring upstream session %q (%q)...",
				cs.lastRenewed.Format(time.RFC822),
				cs.ttl,
				cs.name,
				cs.id,
			)
			if err = cs.destroy(); err != nil {
				cs.log.Printf(
					"maintainTick() - Error destroying expired upstream session %q (%q). This can probably be ignored: %s",
					name,
					sid,
					err,
				)
			}
		} else if err = cs.renew(); err != nil {
			// if error during renewal
			cs.log.Printf("maintainTick() - Unable to renew Consul Session: %s", err)
			// TODO: possibly attempt to destroy the session at this point?  the above timeout test statement
			// should eventually be hit if this continues to fail...
		} else {
			// session should be in a happy state.
			cs.log.Printf("maintainTick() - Upstream session %q (%q) renewed", cs.name, cs.id)
		}
	}

	if cs.id == "" {
		// if this is the first iteration of the loop or if an error occurred above, test and try to create
		// a new session
		if err = cs.create(); err != nil {
			cs.log.Printf("maintainTick() - Unable to create upstream session: %s", err)
		} else {
			cs.log.Printf("maintainTick() - New upstream session %q (%q) created.", cs.name, cs.id)
		}
	}

	cs.mu.Unlock()

	//send update after unlock
	cs.updateWatchers(err)
}

func (cs *Session) shutdown(stopped chan<- error) {
	var (
		sid, name string
		err       error
	)

	cs.mu.Lock()

	cs.log.Printf("shutdown() - Stopping session...")

	// localize most recent upstream session info
	sid = cs.id
	name = cs.name

	if cs.id != "" {
		// if we have a reference to an upstream session id, attempt to destroy it.
		if derr := cs.destroy(); derr != nil {
			msg := fmt.Sprintf("shutdown() - Error destroying upstream session %q (%q) during shutdown: %s", name, sid, derr)
			log.Print(msg)
			if err != nil {
				// if there was an existing error, append this error to it to be sent along the Stop() resp chan
				err = fmt.Errorf("%s; %s", err, msg)
			}
		} else {
			log.Printf("shutdown() - Upstream session %q (%q) destroyed", name, sid)
		}
	}

	// set our state to stopped, preventing further interaction.
	cs.state = SessionStateStopped

	cs.mu.Unlock()

	// just in case...
	if stopped != nil {
		// send along the last seen error, whatever it was.
		stopped <- err
	}

	// send final update
	cs.updateWatchers(err)

	cs.log.Print("shutdown() - Session stopped")
}

// TODO: improve updates to include the action taken this loop, and whether it is the last action to be taken this loop
// i.e., destroy / renew can happen in the same loop as create.
func (cs *Session) maintain() {
	var (
		tick    time.Time
		stopped chan error
	)

	intervalTicker := time.NewTicker(cs.interval)

maintaining:
	for {
		select {
		case tick = <-intervalTicker.C:
			cs.maintainTick(tick)
		case stopped = <-cs.stop:
			break maintaining
		}
	}

	intervalTicker.Stop()

	cs.shutdown(stopped)

	cs.log.Printf("maintain() - Exiting maintain loop")
}

// ParseSessionName is provided so you don't have to parse it yourself :)
func ParseSessionName(name string) (*SessionNameParts, error) {
	split := strings.Split(name, "_")

	switch len(split) {
	case 2:
		return &SessionNameParts{
			NodeName: split[0],
			RandomID: split[1],
		}, nil
	case 3:
		return &SessionNameParts{
			Key:      split[0],
			NodeName: split[1],
			RandomID: split[2],
		}, nil

	default:
		return nil, fmt.Errorf("expected 2 or 3 parts in session name \"%s\", saw only \"%d\"", name, len(split))
	}
}

type SessionWatchFunc func(update SessionUpdate)

type sessionWatchers struct {
	mu    sync.RWMutex
	funcs map[string]SessionWatchFunc
}

func newSessionWatchers() *sessionWatchers {
	w := &sessionWatchers{
		funcs: make(map[string]SessionWatchFunc),
	}
	return w
}

// Watch allows you to register a function that will be called when the election SessionState has changed
func (c *sessionWatchers) Add(id string, fn SessionWatchFunc) string {
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

// Unwatch will remove a function from the list of sessionWatchers.
func (c *sessionWatchers) Remove(id string) {
	c.mu.Lock()
	delete(c.funcs, id)
	c.mu.Unlock()
}

func (c *sessionWatchers) RemoveAll() {
	c.mu.Lock()
	c.funcs = make(map[string]SessionWatchFunc)
	c.mu.Unlock()
}

// notifyWatchers is a thread safe update of leader status
func (c *sessionWatchers) notify(update SessionUpdate) {
	c.mu.RLock()
	for _, fn := range c.funcs {
		go fn(update)
	}
	c.mu.RUnlock()
}
