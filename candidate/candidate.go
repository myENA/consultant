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

		// AutoRun [optional]
		//
		// If set to true, the Candidate will immediately enter its election pool after successful construction
		AutoRun bool
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
	var (
		id, kvKey, sessionTTL string
		client                *api.Client
		autoRun               bool
		err                   error
	)

	if conf != nil {
		kvKey = conf.KVKey
		id = conf.ID
		sessionTTL = conf.SessionTTL
		client = conf.Client
		autoRun = conf.AutoRun
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

	// attempt to create persistent session manager...
	if c.session, err = c.createSession(); err != nil {
		return nil, err
	}

	if autoRun {
		c.log.Debug("AutoRun enabled")
		c.Run()
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

	if !c.Running() {
		return fmt.Errorf("candidate %s is not in running", c.ID())
	}

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
func (c *Candidate) Wait() error {
	return c.WaitFor(1<<63 - 1)
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

// sessionUpdate is the receiver for the session update callback
func (c *Candidate) sessionUpdate(update session.Update) {
	c.mu.Lock()
	if c.session.ID() == update.ID {
		c.mu.Unlock()
		select {
		case c.sessionUpdateChan <- update:
		default:
			c.log.Printf("Unable to push session update onto channel.  Update: %#v")
		}
	} else {
		c.mu.Unlock()
		c.log.Printf("Received update from session %q but our local session is %q...", update.ID, c.session.ID())
	}
}

// acquire will attempt to do just that.  Caller must hold lock!
func (c *Candidate) acquire(sid string) (bool, error) {
	var (
		elected bool
		err     error
	)

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
	// TODO: this could stand for some further cleanup...
	var (
		sid                          string
		elected, updated             bool
		resigned                     chan struct{}
		sessionUpdate                session.Update
		consecutiveSessionErrorCount int
		err                          error
	)

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
			if c.session.Running() {
				// if our session manager is still running
				if sid = c.session.ID(); sid == "" {
					// this should only ever happen very early on in the election process
					elected = false
					updated = c.elected != nil && *c.elected != elected
					c.log.Debugf("Acquire tick: Session does not exist, will try locking again in %d seconds...", int64(interval.Seconds()))
				} else if elected, err = c.acquire(sid); err != nil {
					// most likely hit due to transport error.
					updated = c.elected != nil && *c.elected != elected
					c.log.Printf("Acquire tick: Error attempting to acquire lock: %s", err)
				} else {
					// if c.elected is nil, indicating this is the initial election loop, or if the election state
					// changed mark update as true
					updated = c.elected == nil || *c.elected != elected
				}
			} else {
				// if we are below the threshold, just try to restart existing session
				c.log.Printf("Acquire tick: Session is in stopped state, attempting to restart...")
				elected = false
				updated = c.elected != nil && *c.elected != elected
				c.session.Run()
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
				c.mu.Unlock()

				c.watchers.notify(*up)
			} else {
				c.mu.Unlock()
			}

		case sessionUpdate = <-c.sessionUpdateChan:
			c.mu.Lock()

			if sessionUpdate.Error != nil {
				// if there was an update either creating or renewing our session
				consecutiveSessionErrorCount++
				c.log.Printf("Session Update: Error (%d in a row): %s", consecutiveSessionErrorCount, sessionUpdate.Error)
				if sessionUpdate.State == session.StateRunning && consecutiveSessionErrorCount > 2 {
					// if the session is still running but we've seen more than 2 errors, attempt a stop -> start cycle
					c.log.Print("Session Update: 2 successive errors seen, stopping session")
					if err = c.session.Stop(); err != nil {
						c.log.Printf("Session update: Error stopping session: %s", err)
					}
					elected = false
					updated = c.elected != nil && *c.elected != elected
				}
				// do not modify elected state here unless we've breached the threshold.  could just be a temporary
				// issue
			} else if sessionUpdate.State == session.StateStopped {
				// if somehow the session state became stopped (this should basically never happen...), do not attempt
				// to kickstart session here.  test if we need to update candidate state and notify watchers, then move
				// on.  next acquire tick will attempt to restart session.
				consecutiveSessionErrorCount = 0
				elected = false
				updated = c.elected != nil && *c.elected != elected
				c.log.Printf("Session Update: Stopped state seen: %#v", sessionUpdate)
			} else {
				// if we got a non-error / non-stopped update, there is nothing to do.
				consecutiveSessionErrorCount = 0
				c.log.Debugf("Session Update: Received %#v", sessionUpdate)
			}

			// if updated
			if updated {
				// this should only ever hit if we breach the error threshold or our session stopped running
				if elected {
					c.log.Debug("We have won the election")
				} else {
					c.log.Debug("We have lost the election")
				}
				// update internal state
				*c.elected = elected
				// modify update payload
				up.Elected = elected
				c.mu.Unlock()
				// send notifications after unlocking
				c.watchers.notify(*up)
			} else {
				c.mu.Unlock()
			}

		case resigned = <-c.resign:
			break acquisition
		}
	}

	c.log.Debug("Resigning...")

	acquireTicker.Stop()

	c.mu.Lock()

	// modify internal state
	*c.elected = false
	c.state = StateResigned

	// send notifications
	up.Elected = false
	up.State = StateResigned

	if c.session != nil {
		if err = c.session.Stop(); err != nil {
			c.log.Printf("Error stopping session: %s", err)
		}
	}

	// release lock before the final steps so the object is usable
	c.mu.Unlock()

	// just in case....
	if resigned != nil {
		// notify caller that we've stopped
		resigned <- struct{}{}
	}

	c.watchers.notify(*up)

	c.log.Print("Resigned")
}
