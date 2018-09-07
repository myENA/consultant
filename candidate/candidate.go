package candidate

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hashicorp/consul/api"
	"github.com/myENA/consultant/candidate/session"
	"github.com/myENA/consultant/log"
	"github.com/myENA/consultant/util"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"
)

type State uint8

const (
	StateResigned State = iota
	StateRunning
)

const (
	IDRegex = `^[a-zA-Z0-9:.-]+$` // only allow certain characters in an ID

	SessionKeyPrefix = "candidate-"
	sessionKeyFormat = SessionKeyPrefix + "%s"
)

var (
	validCandidateIDTest = regexp.MustCompile(IDRegex)
	InvalidCandidateID   = fmt.Errorf("candidate ID must obey \"%s\"", IDRegex)
)

type (
	// LeaderKVValue is the body of the acquired KV
	LeaderKVValue struct {
		LeaderAddress string
	}

	// ElectionUpdate is sent to watchers on election state change
	ElectionUpdate struct {
		// Elected tracks whether this specific candidate has been elected
		Elected bool
		// State tracks the current state of this candidate
		State State
	}

	Config struct {
		// KVKey [required]
		//
		// Must be the key to attempt to acquire a session lock on.  This key must be considered ephemeral, and not contain
		// anything you don't want overwritten / destroyed.
		KVKey string

		// ID [suggested]
		//
		// Should be a unique identifier that makes sense within the scope of your implementation.
		// If left blank it will attempt to use the local IP address, otherwise a random string will be generated.
		ID string

		// SessionTTL [optional]
		//
		// The duration of time a given candidate can be elected without re-trying.  A "good" value for this depends
		// entirely upon your implementation.  Keep in mind that once the KVKey lock is acquired with the session, it will
		// remain locked until either the specified session TTL is up or Resign() is explicitly called.
		//
		// If not defined, will default to value of session.DefaultTTL
		SessionTTL string

		// Client [optional]
		//
		// Consul API client.  If not specified, one will be created using api.DefaultConfig()
		Client *api.Client
	}

	Candidate struct {
		mu  sync.Mutex
		log log.DebugLogger

		client *api.Client

		id       string
		session  *session.Session
		watchers *watchers

		kvKey string
		ttl   time.Duration

		elected *bool
		state   State
		resign  chan chan struct{}

		sessionTTL        string
		sessionUpdateChan chan session.Update
	}
)

func New(conf *Config) (*Candidate, error) {
	var id, kvKey, sessionTTL string
	var client *api.Client
	var err error

	if conf != nil {
		kvKey = conf.KVKey
		id = conf.ID
		sessionTTL = conf.SessionTTL
		client = conf.Client
	}

	if kvKey == "" {
		return nil, errors.New("key cannot be empty")
	}

	if client == nil {
		client, err = api.NewClient(api.DefaultConfig())
		if err != nil {
			return nil, fmt.Errorf("unable to create consul api client: %s", err)
		}
	}

	id = strings.TrimSpace(id)
	if id == "" {
		if addr, err := util.MyAddress(); err != nil {
			id = util.RandStr(8)
		} else {
			id = addr
		}
	}

	if !validCandidateIDTest.MatchString(id) {
		return nil, InvalidCandidateID
	}

	c := &Candidate{
		log:      log.New(fmt.Sprintf("candidate-%s", id)),
		client:   client,
		id:       id,
		watchers: newWatchers(),
		kvKey:    kvKey,
		resign:   make(chan chan struct{}, 1),

		elected: new(bool),

		sessionUpdateChan: make(chan session.Update, 1),
		sessionTTL:        sessionTTL,
	}

	if c.session, err = c.createSession(); err != nil {
		return nil, err
	}

	return c, nil
}

// Run will enter this candidate into the election pool
func (c *Candidate) Run() {
	c.mu.Lock()
	if c.state == StateRunning {
		c.mu.Unlock()
		return
	}
	c.state = StateRunning
	c.mu.Unlock()

	go c.lockRunner()
}

// Resign will remove this candidate from the election pool
func (c *Candidate) Resign() {
	c.mu.Lock()
	if c.state == StateResigned {
		c.mu.Unlock()
		return
	}
	c.state = StateResigned
	c.mu.Unlock()

	resigned := make(chan struct{}, 1)
	c.resign <- resigned
	<-resigned
	close(resigned)
}

// ID returns the unique identifier given at construct
func (c *Candidate) ID() string {
	return c.id
}

// SessionID is the name of this candidate's session
func (c *Candidate) SessionID() string {
	return c.session.ID()
}

// SessionTTL returns the parsed TTL
func (c *Candidate) SessionTTL() time.Duration {
	return c.session.TTL()
}

// Elected will return true if this candidate's session is "locking" the kv
func (c *Candidate) Elected() bool {
	c.mu.Lock()
	var el bool
	if c.elected != nil {
		el = *c.elected
	}
	c.mu.Unlock()
	return el
}

// LeaderService will attempt to locate the leader's session entry in your local agent's datacenter
func (c *Candidate) LeaderService() (*api.SessionEntry, error) {
	return c.ForeignLeaderService("")
}

// Return the leader, assuming its ID can be interpreted as an IP address
func (c *Candidate) LeaderIP() (net.IP, error) {
	return c.ForeignLeaderIP("")

}

// ForeignLeaderIP will attempt to parse the body of the locked kv key to locate the current leader
func (c *Candidate) ForeignLeaderIP(dc string) (net.IP, error) {

	kv, _, err := c.client.KV().Get(c.kvKey, &api.QueryOptions{Datacenter: dc})
	if err != nil {
		return nil, err
	} else if kv == nil || len(kv.Value) == 0 {
		return nil, errors.New("no leader has been elected")
	}

	info := &LeaderKVValue{}
	if err = json.Unmarshal(kv.Value, info); err == nil && info.LeaderAddress != "" {
		if ip := net.ParseIP(info.LeaderAddress); ip != nil {
			return ip, nil
		}
	}
	return nil, fmt.Errorf("key \"%s\" had unexpected value \"%s\" for \"LeaderAddress\"", c.kvKey, string(kv.Value))
}

// ForeignLeaderService will attempt to locate the leader's session entry in a datacenter of your choosing
func (c *Candidate) ForeignLeaderService(dc string) (*api.SessionEntry, error) {
	var kv *api.KVPair
	var se *api.SessionEntry
	var err error

	kv, _, err = c.client.KV().Get(c.kvKey, &api.QueryOptions{Datacenter: dc})
	if err != nil {
		return nil, err
	}

	if nil == kv {
		return nil, fmt.Errorf("kv \"%s\" not found in datacenter \"%s\"", c.kvKey, dc)
	}

	if kv.Session != "" {
		se, _, err = c.client.Session().Info(kv.Session, &api.QueryOptions{Datacenter: dc})
		if nil != se {
			return se, nil
		}
	}

	return nil, fmt.Errorf("kv \"%s\" has no session in datacenter \"%s\"", c.kvKey, dc)
}

// Watch allows you to register a function that will be called when the election State has changed
func (c *Candidate) Watch(id string, fn WatchFunc) string {
	return c.watchers.Add(id, fn)
}

// Unwatch will remove a function from the list of watchers.
func (c *Candidate) Unwatch(id string) {
	c.watchers.Remove(id)
}

// RemoveWatchers will clear all watchers
func (c *Candidate) RemoveWatchers() {
	c.watchers.RemoveAll()
}

// UpdateWatchers will immediately push the current state of this Candidate to all currently registered Watchers
func (c *Candidate) UpdateWatchers() {
	c.mu.Lock()
	var el bool
	if c.elected != nil {
		el = *c.elected
	}
	up := ElectionUpdate{
		Elected: el,
		State:   c.state,
	}
	c.watchers.notify(up)
	c.mu.Unlock()
}

// WaitFor will wait for a candidate to be elected or until duration has passed
func (c *Candidate) WaitFor(td time.Duration) error {
	var err error

	timer := time.NewTimer(td)

waitLoop:
	for {
		select {
		case <-timer.C:
			err = errors.New("expire time breached")
			// attempt to locate current leader
		default:
			if _, err = c.LeaderService(); nil == err {
				break waitLoop
			}
			c.log.Debugf("Error locating leader service: %s", err)
		}

		time.Sleep(time.Second * 1)
	}

	if !timer.Stop() {
		<-timer.C
	}

	return err
}

// WaitUntil will for a candidate to be elected or until the deadline is breached
func (c *Candidate) WaitUntil(t time.Time) error {
	now := time.Now()
	if now.After(t) {
		return errors.New("\"t\" must represent a time in the future")
	}

	return c.WaitFor(t.Sub(now))
}

// Wait will block until a leader has been elected, regardless of candidate.
func (c *Candidate) Wait() {
	c.WaitFor(1<<63 - 1)
}

func (c *Candidate) State() State {
	c.mu.Lock()
	s := c.state
	c.mu.Unlock()
	return s
}

func (c *Candidate) Running() bool {
	return c.State() == StateRunning
}

func (c *Candidate) sessionUpdate(update session.Update) {
	if c.session != nil && c.session.ID() == update.ID {
		if c.Running() {
			c.sessionUpdateChan <- update
		} else {
			c.log.Printf("We are no longer in the running, cannot process session update: %#v", update)
		}
	} else {
		c.log.Printf("Received update from rogue session: %s", update.ID)
	}
}

// acquire will attempt to do just that.  Caller must hold lock!
func (c *Candidate) acquire(sid string) (bool, error) {
	var err error
	var elected bool

	kvpValue := &LeaderKVValue{
		LeaderAddress: c.id,
	}

	kvp := &api.KVPair{
		Key:     c.kvKey,
		Session: sid,
	}

	kvp.Value, err = json.Marshal(kvpValue)
	if err != nil {
		c.log.Printf("Unable to marshal KV body: %s", err)
	}

	elected, _, err = c.client.KV().Acquire(kvp, nil)
	return elected, err
}

func (c *Candidate) createSession() (*session.Session, error) {
	sessionConfig := &session.Config{
		Key:        fmt.Sprintf(sessionKeyFormat, c.id),
		TTL:        c.sessionTTL,
		Behavior:   api.SessionBehaviorDelete,
		Log:        c.log,
		Client:     c.client,
		UpdateFunc: c.sessionUpdate,
	}
	return session.New(sessionConfig)
}

func (c *Candidate) lockRunner() {
	var sid string
	var elected bool
	var resigned chan struct{}
	var updated bool
	var sessionUpdate session.Update
	var err error

	sessionErrorsSeen, sessionStoppedUpdatesSeen := 0, 0

	// run initial session
	c.session.Run()

	// this is a long-lived object whose state is updated per iteration below.
	up := &ElectionUpdate{
		State: StateRunning,
	}

	interval := c.session.RenewInterval()

	acquireTicker := time.NewTicker(interval)

acquisition:
	for {
		updated = false
		select {
		case <-acquireTicker.C:
			c.mu.Lock()

			if c.session == nil {
				// it is possible for the session to be nil if the sessionUpdate case is unable to recreate upon error
				// threshold being met

				// set elected state to false, will be updated later.
				elected = false

				// only send an update if a previous election attempt was successful and our state changed
				updated = c.elected != nil && *c.elected != elected

				c.log.Print("Acquire tick: No session, will try to create...")
				if c.session, err = c.createSession(); err != nil {
					c.log.Printf("Acquire tick: Error creating session, will try again in %d seconds. Err: %s", int64(interval.Seconds()), err)
				} else {
					c.session.Run()
					c.log.Printf("Acquire tick: Session created successfully")
				}
			} else if sid = c.session.ID(); sid == "" {
				// this should only ever happen very early on in the election process
				elected = false
				updated = c.elected != nil && *c.elected != elected
				c.log.Debugf("Acquire tick: Session does not exist, will try locking again in %d seconds...", int64(interval.Seconds()))
			} else if elected, err = c.acquire(sid); err != nil {
				// most likely hit due to transport error.
				elected = false
				updated = c.elected != nil && *c.elected != elected
				c.log.Printf("Acquire tick: Error attempting to acquire lock: %s", err)
			} else {
				updated = c.elected == nil || *c.elected != elected
			}

			c.mu.Unlock()

		case sessionUpdate = <-c.sessionUpdateChan:
			c.mu.Lock()

			// if there was an update either creating or renewing our session
			if sessionUpdate.Error != nil {
				sessionErrorsSeen++
				sessionStoppedUpdatesSeen = 0

				c.log.Printf("Session Update: Error (%d in a row): %s", sessionErrorsSeen, sessionUpdate.Error)

				// if we breach this threshold, stop our current session and attempt to make a new one next pass
				if sessionErrorsSeen >= 2 {
					c.log.Print("Session Update: 2 successive errors seen, will construct new session")
					c.session.Stop()
					elected = false
					c.session = nil
				}
			} else if sessionUpdate.State == session.StateStopped {
				sessionStoppedUpdatesSeen++
				sessionErrorsSeen = 0

				elected = false

				c.log.Printf("Session Update: Stopped state seen (%d in row): %#v", sessionStoppedUpdatesSeen, sessionUpdate)

				if sessionStoppedUpdatesSeen >= 2 {
					c.log.Print("Session Update: Stopped state seen 2 successive times, will construct new session")
					if c.session, err = c.createSession(); err != nil {
						c.log.Printf("Unable to recreate  will try again in %d seconds.  Err: %s.", int64(interval.Seconds()), err)
					} else {
						c.log.Print("Session created successfully")
						c.session.Run()
					}
				}
			} else {
				c.log.Debugf("Session Update: Received %#v", sessionUpdate)
			}

			updated = c.elected != nil && *c.elected != elected

			c.mu.Unlock()

		case resigned = <-c.resign:
			break acquisition
		}

		// if updated
		if updated {
			if elected {
				c.log.Debug("We have won the election")
			} else {
				c.log.Debug("We have lost the election")
			}

			// update internal state
			*c.elected = elected

			// send notifications
			up.Elected = elected
			c.watchers.notify(*up)
		}
	}

	c.log.Debug("Resigning...")

	acquireTicker.Stop()

	c.mu.Lock()

	// modify state
	*c.elected = false
	c.state = StateResigned

	// send notifications
	up.Elected = false
	up.State = StateResigned
	c.watchers.notify(*up)

	done := make(chan struct{})

	// test for session nil, if so spin up new routine that waits for session to be destroyed.
	// TODO: do something more clever with the session's update system instead of this chan?
	if c.session != nil {
		go func() {
			// stop session, this might block for a bit
			c.session.Stop()
			done <- struct{}{}
		}()
	} else {
		done <- struct{}{}
	}

	// release lock before the final steps so the object is usable
	c.mu.Unlock()

	// if need be, wait for session to term
	<-done
	close(done)

	// notify our caller that we've finished with resignation
	if resigned != nil {
		resigned <- struct{}{}
	}

	c.log.Print("Resigned")
}
