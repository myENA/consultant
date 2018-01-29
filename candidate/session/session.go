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

type (
	Update struct {
		ID      string
		Name    string
		Renewed time.Time
		Error   error
	}

	UpdateFunc func(Update)

	Session struct {
		mu     sync.Mutex
		log    log.DebugLogger
		client *api.Client

		node        string
		candidateID string

		id          string
		name        string
		ttl         time.Duration
		interval    time.Duration
		lastRenewed time.Time

		stop  chan chan struct{}
		state State

		onUpdate UpdateFunc
	}
)

func New(log log.DebugLogger, client *api.Client, candidateID, ttl string, onUpdate UpdateFunc) (*Session, error) {
	var err error

	if client == nil {
		client, err = api.NewClient(api.DefaultConfig())
		if err != nil {
			return nil, fmt.Errorf("no Consul api client provided and unable to create: %s", err)
		}
	}

	if ttl == "" {
		ttl = "30s"
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

	interval := time.Duration(ttlTD.Nanoseconds() / 2)
	if interval.Nanoseconds() < int64(time.Second) {
		interval = time.Second
	}

	log.Debugf("Lock interval: %d seconds", int64(ttlTD.Seconds()))
	log.Debugf("Session renew interval: %d seconds", int64(interval.Seconds()))

	cs := &Session{
		log:         log,
		client:      client,
		candidateID: candidateID,
		ttl:         ttlTD,
		interval:    interval,
		stop:        make(chan chan struct{}, 1),
		onUpdate:    onUpdate,
	}

	if cs.node, err = client.Agent().NodeName(); err != nil {
		return nil, fmt.Errorf("unable to determine node: %s", err)
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
	cs.mu.Lock()
	ttl := cs.ttl
	cs.mu.Unlock()
	return ttl
}

func (cs *Session) RenewInterval() time.Duration {
	cs.mu.Lock()
	i := cs.interval
	cs.mu.Unlock()
	return i
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
func (cs *Session) sendUpdate(err error) {
	if cs.onUpdate == nil {
		return
	}

	cs.onUpdate(Update{
		ID:      cs.id,
		Name:    cs.name,
		Renewed: cs.lastRenewed,
		Error:   err,
	})
}

// create will attempt to do just that. Caller MUST hold lock!
func (cs *Session) create() error {
	name := fmt.Sprintf("leader-%s-%s-%s", cs.candidateID, cs.node, util.RandStr(12))

	cs.log.Debugf("Attempting to create Consul Session \"%s\"...", name)

	se := &api.SessionEntry{
		Name:     name,
		Behavior: api.SessionBehaviorDelete,
		TTL:      cs.ttl.String(),
	}

	sid, _, err := cs.client.Session().Create(se, nil)

	if err != nil {
		cs.id = ""
		cs.name = ""
	} else {
		cs.id = sid
		cs.name = name
		cs.lastRenewed = time.Now()
	}

	cs.sendUpdate(err)

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
	} else {
		cs.id = se.ID
		cs.lastRenewed = time.Now()
	}

	cs.sendUpdate(err)

	return err
}

func (cs *Session) maintain() {
	var stopped chan struct{}
	var err error

	// try to create session immediately
	cs.mu.Lock()
	if err = cs.create(); err != nil {
		cs.log.Printf(
			"Unable to perform initial session creation, will try again in \"%d\" seconds: %s",
			int64(cs.interval.Seconds()),
			err)
	}
	cs.mu.Unlock()

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
			cs.mu.Unlock()

		case stopped = <-cs.stop:
			break maintaining
		}
	}

	cs.mu.Lock()

	intervalTicker.Stop()

	if cs.id != "" {
		if _, err = cs.client.Session().Destroy(cs.id, nil); err != nil {
			cs.log.Printf("Unable to destroy session \"%s\": %s", cs.id, err)
		}
	}

	cs.id = ""
	cs.name = ""
	cs.lastRenewed = time.Time{}

	// for good measure...
	cs.state = StateStopped

	if stopped != nil {
		stopped <- struct{}{}
	}

	cs.mu.Unlock()
}
