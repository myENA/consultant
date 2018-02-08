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

		stop  chan chan struct{}
		state State

		updateFunc UpdateFunc
	}
)

func New(conf *Config) (*Session, error) {
	var key, ttl, behavior string
	var client *api.Client
	var l log.DebugLogger
	var err error

	if conf != nil {
		key = conf.Key
		ttl = conf.TTL
		behavior = conf.Behavior
		l = conf.Log
		client = conf.Client
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
		interval:   time.Duration(ttlTD.Nanoseconds() / 2),
		stop:       make(chan chan struct{}, 1),
		updateFunc: conf.UpdateFunc,
	}

	if cs.node, err = client.Agent().NodeName(); err != nil {
		return nil, fmt.Errorf("unable to determine node: %s", err)
	}

	l.Debugf("Lock interval: %d seconds", int64(ttlTD.Seconds()))
	l.Debugf("Session renew interval: %d seconds", int64(cs.interval.Seconds()))

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
		cs.mu.Unlock()
		return
	}

	cs.state = StateRunning

	// try to create session immediately
	if err := cs.create(); err != nil {
		cs.log.Printf(
			"Unable to perform initial session creation, will try again in \"%d\" seconds: %s",
			int64(cs.interval.Seconds()),
			err)
	}

	cs.mu.Unlock()

	go cs.maintain()
}

func (cs *Session) Stop() {
	cs.mu.Lock()
	if cs.state == StateStopped {
		cs.mu.Unlock()
		return
	}
	cs.state = StateStopped
	cs.mu.Unlock()

	stopped := make(chan struct{}, 1)
	cs.stop <- stopped
	<-stopped
	close(stopped)
}

func (cs *Session) State() State {
	cs.mu.Lock()
	s := cs.state
	cs.mu.Unlock()
	return s
}

// sendUpdate will attempt to notify whoever cares that the session has been created / refreshed / errored
// Caller MUST hold lock!
func (cs *Session) sendUpdate(up Update) {
	if cs.updateFunc == nil {
		return
	}
	cs.updateFunc(up)
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

func (cs *Session) maintain() {
	var stopped chan struct{}
	var up Update
	var err error

	intervalTicker := time.NewTicker(cs.interval)

maintaining:
	for {
		select {
		case <-intervalTicker.C:
			cs.mu.Lock()
			if cs.id == "" {
				if err = cs.create(); err != nil {
					cs.log.Printf("Unable to create Consul Session: %s", err)
				}
			} else if err = cs.renew(); err != nil {
				cs.log.Printf("Unable to renew Consul Session: %s", err)
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
			cs.sendUpdate(up)

		case stopped = <-cs.stop:
			break maintaining
		}
	}

	cs.log.Debug("Stopping session...")

	intervalTicker.Stop()

	cs.mu.Lock()

	// store most recent session id
	sid := cs.id

	// modify state
	cs.id = ""
	cs.name = ""
	cs.state = StateStopped

	// prepare final update
	up = Update{LastRenewed: cs.lastRenewed, State: StateStopped}

	cs.mu.Unlock()

	// if there was a session id, attempt to destroy it.
	if sid != "" {
		if _, err = cs.client.Session().Destroy(sid, nil); err != nil {
			cs.log.Printf("Unable to destroy session \"%s\": %s", sid, err)
		}
	}

	// just in case...
	if stopped != nil {
		stopped <- struct{}{}
	}

	// send final update asynchronously, don't much care if they're around to receive it.
	go cs.sendUpdate(up)

	cs.log.Print("Session stopped")
}
