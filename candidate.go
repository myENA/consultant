package consultant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
)

type State uint8

const (
	CandidateStateResigned State = iota
	CandidateStateRunning
)

const (
	CandidateIDRegex = `^[a-zA-Z0-9:.-]+$` // only allow certain characters in an ID

	CandidateSessionKeyPrefix = "candidate-"
	CandidateSessionKeyFormat = CandidateSessionKeyPrefix + "%s"
)

var (
	candidateValidIDTest  = regexp.MustCompile(CandidateIDRegex)
	CandidateInvalidIDErr = fmt.Errorf("candidate ID must obey \"%s\"", CandidateIDRegex)
)

type (
	// CandidateLeaderKVValue is the body of the acquired KV
	CandidateLeaderKVValue struct {
		LeaderAddress string `json:"leader_address"`
	}

	// CandidateElectionUpdate is sent to watchers on election state change
	CandidateElectionUpdate struct {
		// Elected tracks whether this specific candidate has been elected
		Elected bool `json:"elected"`
		// State tracks the current state of this candidate
		State State `json:"state"`
	}

	CandidateConfig struct {
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

		// APIConfig [optional]
		//
		// Consul API Client configuration.  If set, one will be created using api.DefaultConfig()
		APIConfig *api.Config

		// StartImmediately [optional]
		//
		// If set to true, the Candidate will immediately enter its election pool after successful construction
		AutoRun bool

		// Debug [optional]
		//
		// Enables debug logging output
		Debug bool
	}

	Candidate struct {
		mu sync.RWMutex

		id       string
		watchers *candidateWatchers
		kvKey    string
		ttl      time.Duration
		elected  *bool
		state    State

		session    *ManagedSession
		sessionTTL string

		apiConfig *api.Config
		client    *api.Client

		logger Logger
		dbg    bool

		stop chan chan struct{}
	}
)

func NewCandidate(ctx context.Context, conf *CandidateConfig, logger Logger) (*Candidate, error) {
	var (
		id        string
		apiConfig *api.Config
		err       error

		c = new(Candidate)
	)

	if conf == nil {
		return nil, errors.New("conf cannot be nil")
	}
	if conf.KVKey == "" {
		return nil, errors.New("conf.KVKey cannot be empty")
	}

	if conf.ID == "" {
		if addr, err := LocalAddress(); err != nil {
			id = LazyRandomString(8)
		} else {
			id = addr
		}
	} else {
		id = conf.ID
	}

	if !candidateValidIDTest.MatchString(id) {
		return nil, CandidateInvalidIDErr
	}

	if conf.APIConfig != nil {
		apiConfig = conf.APIConfig
	} else {
		apiConfig = api.DefaultConfig()
	}

	c.id = id
	c.watchers = newCandidateWatchers()
	c.logger = logger
	c.kvKey = conf.KVKey
	c.elected = new(bool)
	c.stop = make(chan chan struct{}, 1)

	if conf.SessionTTL != "" {
		c.sessionTTL = conf.SessionTTL
	} else {
		c.sessionTTL = api.DefaultLockSessionTTL
	}

	if c.client, err = api.NewClient(apiConfig); err != nil {
		return nil, fmt.Errorf("unable to create consul api client: %s", err)
	}

	if conf.AutoRun {
		c.logf(true, "StartImmediately enabled")
		if err := c.Run(); err != nil {
			return nil, fmt.Errorf("error occurred during auto run: %s", err)
		}
	}

	return c, nil
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
	c.mu.RLock()
	var el bool
	if c.elected != nil {
		el = *c.elected
	}
	c.mu.RUnlock()
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

	info := &CandidateLeaderKVValue{}
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
func (c *Candidate) Watch(id string, fn CandidateWatchFunc) string {
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
	c.mu.RLock()
	var el bool
	if c.elected != nil {
		el = *c.elected
	}
	up := CandidateElectionUpdate{
		Elected: el,
		State:   c.state,
	}
	c.watchers.notify(up)
	c.mu.RUnlock()
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
			c.logf(false, "Error locating leader service: %s", err)
		}

		time.Sleep(time.Second)
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

// Wait will block until a leader has been elected, regardless of Candidate
func (c *Candidate) Wait() error {
	return c.WaitFor(1<<63 - 1)
}

func (c *Candidate) State() State {
	c.mu.RLock()
	s := c.state
	c.mu.RUnlock()
	return s
}

func (c *Candidate) Running() bool {
	return c.State() == CandidateStateRunning
}

// Run will enter this candidate into the election pool
func (c *Candidate) Run() error {
	c.mu.Lock()
	if c.state == CandidateStateRunning {
		c.mu.Unlock()
		return nil
	}

	var err error

	// create new session
	if c.session, err = c.createSession(); err != nil {
		c.mu.Unlock()
		return fmt.Errorf("unable to create session: %s", err)
	}

	// kickstart session
	c.session.Run()

	// start up the lock maintainer
	go c.maintainLock()

	c.state = CandidateStateRunning

	c.mu.Unlock()

	return nil
}

// Resign will remove this candidate from the election pool
func (c *Candidate) Resign() {
	c.mu.Lock()
	if c.state == CandidateStateResigned {
		c.mu.Unlock()
		return
	}

	c.log.Print("Resigning...")

	// wait for lock maintenance loop to stop
	done := make(chan struct{}, 1)
	c.stop <- done
	<-done
	close(done)

	// only update elected state if we were ever elected in the first place.
	if c.elected != nil {
		*c.elected = false
	}
	c.state = CandidateStateResigned

	if c.session != nil {
		if err := c.session.Stop(); err != nil {
			c.logf(false, "Error stopping session during resign: %s", err)
		}
	}

	c.mu.Unlock()

	c.logf(false, "Resigned")

	// notify watchers of updated state
	c.watchers.notify(CandidateElectionUpdate{false, CandidateStateResigned})
}

func (c *Candidate) logf(debug bool, f string, v ...interface{}) {
	if c.logger != nil || debug && !c.dbg {
		return
	}
	c.logger.Printf(f, v...)
}

// acquire will attempt to do just that.  Caller must hold lock!
func (c *Candidate) acquire(sid string) (bool, error) {
	var (
		elected bool
		err     error
	)

	kvpValue := &CandidateLeaderKVValue{
		LeaderAddress: c.id,
	}

	kvp := &api.KVPair{
		Key:     c.kvKey,
		Session: sid,
	}

	kvp.Value, err = json.Marshal(kvpValue)
	if err != nil {
		c.logf(false, "Unable to marshal KV body: %s", err)
	}

	elected, _, err = c.client.KV().Acquire(kvp, nil)
	return elected, err
}

func (c *Candidate) createSession() (*ManagedSession, error) {
	sessionConfig := ManagedSessionConfig{
		Key:         fmt.Sprintf(CandidateSessionKeyFormat, c.id),
		TTL:         c.sessionTTL,
		TTLBehavior: api.SessionBehaviorDelete,
		Log:         c.log,
		Client:      c.client,
		UpdateFunc:  c.sessionUpdate,
	}
	return NewManagedSession(&sessionConfig)
}

// refreshLock is responsible for attempting to create / refresh the session lock on the kv
func (c *Candidate) refreshLock() {
	var (
		sid              string
		elected, updated bool
		err              error
	)
	c.mu.Lock()
	if c.session.Running() {
		// if our session manager is still running
		if sid = c.session.ID(); sid == "" {
			// this should only ever happen very early on in the election process
			elected = false
			updated = c.elected != nil && *c.elected != elected
			c.log.Printf("refreshLock() - ManagedSession does not exist, will try locking again in %d seconds...", int64(c.session.RenewInterval().Seconds()))
		} else if elected, err = c.acquire(sid); err != nil {
			// most likely hit due to transport error.
			updated = c.elected != nil && *c.elected != elected
			c.log.Printf("refreshLock() - Error attempting to acquire lock: %s", err)
		} else {
			// if c.elected is nil, indicating this is the initial election loop, or if the election state
			// changed mark update as true
			updated = c.elected == nil || *c.elected != elected
		}
	} else {
		c.log.Print("refreshLock() - ManagedSession is in stopped state, attempting to restart...")
		elected = false
		updated = c.elected != nil && *c.elected != elected
		c.session.Run()
	}

	// if election state changed
	if updated {
		if elected {
			c.log.Printf("We have won the election")
		} else {
			c.log.Printf("We have lost the election")
		}

		// update internal state
		*c.elected = elected
		c.watchers.notify(CandidateElectionUpdate{State: CandidateStateRunning, Elected: elected})
	}
	c.mu.Unlock()
}

// maintainLock is responsible for triggering the routine that attempts to create / re-acquire the session kv lock
func (c *Candidate) maintainLock() {
	c.log.Printf("maintainLock() - Starting lock maintenance loop")
	var (
		drop chan struct{}

		interval = c.session.RenewInterval()
		ticker   = time.NewTicker(interval)
	)
Locker:
	for {
		select {
		case <-ticker.C:
			c.refreshLock()
		case drop = <-c.stop:
			break Locker
		}
	}
	ticker.Stop()
	drop <- struct{}{}
	c.log.Print("maintainLock() - Exiting lock maintenance loop")
	// and roll...
}

// sessionUpdate is the receiver for the session update callback
func (c *Candidate) sessionUpdate(update ManagedSessionUpdate) {
	if !c.Running() {
		c.log.Printf("sessionUpdate() - Not in the running but received update: %v", update)
		return
	}
	c.mu.RLock()
	if c.session.ID() != update.ID {
		c.mu.RUnlock()
		c.log.Printf("sessionUpdate() - Received update from session %q but our local session is %q...", update.ID, c.session.ID())
		return
	}
	c.mu.RUnlock()
	var (
		consecutiveSessionErrorCount int
		refresh                      bool
		err                          error
	)
	if update.Error != nil {
		// if there was an update either creating or renewing our session
		consecutiveSessionErrorCount++
		c.log.Printf("sessionUpdate() - Error (%d in a row): %s", consecutiveSessionErrorCount, update.Error)
		if update.State == SessionStateRunning && consecutiveSessionErrorCount > 2 {
			// if the session is still running but we've seen more than 2 errors, attempt a stop -> start cycle
			c.log.Print("sessionUpdate() - 2 successive errors seen, stopping session")
			if err = c.session.Stop(); err != nil {
				c.log.Printf("sessionUpdate() - Error stopping session: %s", err)
			}
			refresh = true
		}
		// do not modify elected state here unless we've breached the threshold.  could just be a temporary
		// issue
	} else if update.State == SessionStateStopped {
		// if somehow the session state became stopped (this should basically never happen...), do not attempt
		// to kickstart session here.  test if we need to update candidate state and notify watchers, then move
		// on.  next acquire tick will attempt to restart session.
		consecutiveSessionErrorCount = 0
		refresh = true
		c.log.Printf("sessionUpdate() - Stopped state seen: %#v", update)
	} else {
		// if we got a non-error / non-stopped update, there is nothing to do.
		consecutiveSessionErrorCount = 0
		c.log.Printf("sessionUpdate() - Received %#v", update)
	}

	if refresh {
		c.refreshLock()
	}
}

type CandidateNameParts struct {
	SessionNameParts
	CandidateID string
}

func ParseCandidateSessionName(name string) (*CandidateNameParts, error) {
	sn, err := ParseManagedSessionName(name)
	if err != nil {
		return nil, err
	}

	if !strings.HasPrefix(sn.Key, CandidateSessionKeyPrefix) {
		return nil, fmt.Errorf("session key \"%s\" does not have expected prefix \"%s\"", sn.Key, CandidateSessionKeyPrefix)
	}

	return &CandidateNameParts{
		SessionNameParts: *sn,
		CandidateID:      sn.Key[len(CandidateSessionKeyPrefix):],
	}, nil
}

type CandidateWatchFunc func(update CandidateElectionUpdate)

type candidateWatchers struct {
	mu    sync.RWMutex
	funcs map[string]CandidateWatchFunc
}

func newCandidateWatchers() *candidateWatchers {
	w := &candidateWatchers{
		funcs: make(map[string]CandidateWatchFunc),
	}
	return w
}

// Watch allows you to register a function that will be called when the election State has changed
func (c *candidateWatchers) Add(id string, fn CandidateWatchFunc) string {
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

// Unwatch will remove a function from the list of watchers.
func (c *candidateWatchers) Remove(id string) {
	c.mu.Lock()
	delete(c.funcs, id)
	c.mu.Unlock()
}

func (c *candidateWatchers) RemoveAll() {
	c.mu.Lock()
	c.funcs = make(map[string]CandidateWatchFunc)
	c.mu.Unlock()
}

// notifyWatchers is a thread safe update of leader status
func (c *candidateWatchers) notify(update CandidateElectionUpdate) {
	c.mu.RLock()
	for _, fn := range c.funcs {
		go fn(update)
	}
	c.mu.RUnlock()
}
