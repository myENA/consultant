package session

import (
	"fmt"
	"github.com/hashicorp/consul/api"
	"github.com/myENA/consultant/log"
	"github.com/myENA/consultant/util"
	"github.com/pkg/errors"
	"sync"
	"time"
)

type State uint8

const (
	StateStopped State = iota
	StateRunning
)

const (
	DefaultTTL = "30s"
)

type (
	Update struct {
		ID          string
		Name        string
		LastRenewed time.Time
		Error       error
		State       State
	}

	UpdateFunc func(Update)

	Config struct {
		// Key [suggested]
		//
		// Implementation-specific Key to be placed in session key.
		Key string

		// TTL [optional]
		//
		// Session TTL, defaults to value of DefaultTTL
		TTL string

		// Behavior [optional]
		//
		// Session timeout behavior, defaults to "release"
		Behavior string

		// Log [optional]
		//
		// Logger for this session.  One will be created if value is empty
		Log log.DebugLogger

		// Client [optional]
		//
		// Consul API client, default will be created if not provided
		Client *api.Client

		// UpdateFunc [optional]
		//
		// Callback to be executed after session state change
		UpdateFunc UpdateFunc

		// AutoRun [optional]
		//
		// Whether the session should start immediately after successful construction
		AutoRun bool
	}

	Session struct {
		mu     sync.Mutex
		log    log.DebugLogger
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
		state State

		updateFunc UpdateFunc
	}
)

func New(conf *Config) (*Session, error) {
	var (
		key, ttl, behavior string
		client             *api.Client
		l                  log.DebugLogger
		updateFunc         UpdateFunc
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

	if l == nil {
		if key == "" {
			l = log.New(fmt.Sprintf("session-%s", util.RandStr(8)))
		} else {
			l = log.New(fmt.Sprintf("session-%s", key))
		}
	}

	if ttl == "" {
		ttl = DefaultTTL
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
		log:        l,
		client:     client,
		key:        key,
		ttl:        ttlTD,
		behavior:   behavior,
		interval:   time.Duration(int64(ttlTD) / 2),
		stop:       make(chan chan error, 1),
		updateFunc: updateFunc,
	}

	if cs.node, err = client.Agent().NodeName(); err != nil {
		return nil, fmt.Errorf("unable to determine node: %s", err)
	}

	l.Debugf("Lock interval: %d seconds", int64(ttlTD.Seconds()))
	l.Debugf("Session renew interval: %d seconds", int64(cs.interval.Seconds()))

	if autoRun {
		l.Debug("AutoRun enabled")
		cs.Run()
	}

	return cs, nil
}

func (cs *Session) ID() string {
	cs.mu.Lock()
	sid := cs.id
	cs.mu.Unlock()
	return sid
}

func (cs *Session) Name() string {
	cs.mu.Lock()
	name := cs.name
	cs.mu.Unlock()
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
	cs.mu.Lock()
	t := cs.lastRenewed
	cs.mu.Unlock()
	return t
}

func (cs *Session) Running() bool {
	cs.mu.Lock()
	b := cs.state == StateRunning
	cs.mu.Unlock()
	return b
}

func (cs *Session) Run() {
	cs.mu.Lock()
	if cs.state == StateRunning {
		// if our state is already running, just continue to do so.
		cs.log.Debug("Run() called but I'm already running")
		cs.mu.Unlock()
		return
	}

	// modify state
	cs.state = StateRunning

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
	if cs.state == StateStopped {
		cs.log.Debug("Stop() called but I'm already stopped")
		cs.mu.Unlock()
		return nil
	}
	cs.state = StateStopped
	cs.mu.Unlock()

	stopped := make(chan error, 1)
	cs.stop <- stopped
	err := <-stopped
	close(stopped)
	return err
}

func (cs *Session) State() State {
	cs.mu.Lock()
	s := cs.state
	cs.mu.Unlock()
	return s
}

// create will attempt to do just that. Caller MUST hold lock!
func (cs *Session) create() error {
	var name string

	if cs.key == "" {
		name = fmt.Sprintf("%s_%s", cs.node, util.RandStr(12))
	} else {
		name = fmt.Sprintf("%s_%s_%s", cs.key, cs.node, util.RandStr(12))
	}

	cs.log.Debugf("Attempting to create Consul Session \"%s\"...", name)

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

// TODO: improve updates to include the action taken this loop, and whether it is the last action to be taken this loop
// i.e., destroy / renew can happen in the same loop as create.
func (cs *Session) maintain() {
	var (
		sid, name string
		stopped   chan error
		up        Update
		err       error
	)

	intervalTicker := time.NewTicker(cs.interval)

maintaining:
	for {
		select {
		case <-intervalTicker.C:
			cs.mu.Lock()
			if cs.id != "" {
				// if we were previously able to create an upstream session...
				sid, name = cs.id, cs.name
				if !cs.lastRenewed.IsZero() && time.Now().Sub(cs.lastRenewed) > cs.ttl {
					// if we have a session but the last time we were able to successfully renew it was beyond the TTL,
					// attempt to destroy and allow re-creation down below
					cs.log.Printf(
						"Last renewed time (%s) is > ttl (%s), expiring upstream session %q (%q)...",
						cs.lastRenewed.Format(time.RFC822),
						cs.ttl,
						cs.name,
						cs.id,
					)
					if err = cs.destroy(); err != nil {
						cs.log.Debugf(
							"Error destroying expired upstream session %q (%q). This can probably be ignored: %s",
							name,
							sid,
							err,
						)
					}
				} else if err = cs.renew(); err != nil {
					// if error during renewal
					cs.log.Printf("Unable to renew Consul Session: %s", err)
					// TODO: possibly attempt to destroy the session at this point?  the above timeout test statement
					// should eventually be hit if this continues to fail...
				} else {
					// session should be in a happy state.
					cs.log.Debugf("Upstream session %q (%q) renewed", cs.name, cs.id)
				}
			}

			if cs.id == "" {
				// if this is the first iteration of the loop or if an error occurred above, test and try to create
				// a new session
				if err = cs.create(); err != nil {
					cs.log.Printf("Unable to create upstream session: %s", err)
				} else {
					cs.log.Debugf("New upstream session %q (%q) created.", cs.name, cs.id)
				}
			}

			// create update behind lock
			up = Update{
				ID:          cs.id,
				Name:        cs.name,
				LastRenewed: cs.lastRenewed,
				Error:       err,
				State:       cs.state,
			}

			cs.mu.Unlock()

			//send update after unlock
			// TODO: should this block?
			go sendUpdate(cs.updateFunc, up)

		case stopped = <-cs.stop:
			break maintaining
		}
	}

	cs.log.Debug("Stopping session...")

	intervalTicker.Stop()

	cs.mu.Lock()

	// localize most recent upstream session info
	sid = cs.id
	name = cs.name
	lastRenewed := cs.lastRenewed

	if cs.id != "" {
		// if we have a reference to an upstream session id, attempt to destroy it.
		if derr := cs.destroy(); derr != nil {
			msg := fmt.Sprintf("Error destroying upstream session %q (%q) during shutdown: %s", name, sid, derr)
			log.Print(msg)
			if err != nil {
				// if there was an existing error, append this error to it to be sent along the Stop() resp chan
				err = fmt.Errorf("%s; %s", err, msg)
			}
		} else {
			log.Printf("Upstream session %q (%q) destroyed", name, sid)
		}
	}

	// set our state to stopped, preventing further interaction.
	cs.state = StateStopped

	// prepare final update
	up = Update{LastRenewed: lastRenewed, State: StateStopped}

	cs.mu.Unlock()

	// just in case...
	if stopped != nil {
		// send along the last seen error, whatever it was.
		stopped <- err
	}

	// send final update
	go sendUpdate(cs.updateFunc, up)

	cs.log.Print("Session stopped")
}
