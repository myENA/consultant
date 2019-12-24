package consultant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/consul/api"
)

type CandidateState uint8

const (
	CandidateStateResigned CandidateState = iota
	CandidateStateRunning
)

// CandidateLeaderKVValue is the body of the acquired KV
type CandidateLeaderKVValue struct {
	LeaderID      string    `json:"leader_id"`
	LeaderAddress string    `json:"leader_address"`
	LeaderSince   time.Time `json:"leader_since"`
}

// CandidateUpdate is the value of .Data in all Notification pushes from a Candidate
type CandidateUpdate struct {
	// Elected tracks whether this specific candidate has been elected
	Elected bool `json:"elected"`
	// State tracks the current state of this candidate
	State CandidateState `json:"state"`
}

// CandidateConfig describes a Candidate
type CandidateConfig struct {
	ManagedSessionConfig

	// KVKey [required]
	//
	// Must be the key to attempt to acquire a session lock on.  This key must be considered ephemeral, and not contain
	// anything you don't want overwritten / destroyed.
	KVKey string

	// CandidateID [suggested]
	//
	// Should be a unique identifier for this specific Candidate that makes sense within the scope of your
	// implementation. If left blank it will attempt to use the local IP address, otherwise a random string will be
	// generated.  This is a way to identify which Candidate is holding the lock.
	CandidateID string

	// Debug [optional]
	//
	// Enables debug logging output.  If true here but false in ManagedSessionConfig instance only Candidate will have
	// debug logging enabled and vice versa.
	Debug bool

	// Logger [optional]
	//
	// Logger for logging.  No logger means no logging.  Allows for a separate logger instance to be used from the
	// underlying ManagedSession instance.
	Logger Logger
}

// Candidate represents an extension to the ManagedSession type that will additionally attempt to apply the session
// to a specific KV key.  This can then be used to facilitate "leader election" by way of the "leader" being the
// Candidate who's session is locking the target key.
type Candidate struct {
	*notifierBase
	mu sync.RWMutex

	ms *ManagedSession

	id      string
	kvKey   string
	elected *bool
	state   CandidateState

	dbg    bool
	logger Logger

	consecutiveSessionErrors *uint64
	stop                     chan chan struct{}
}

func NewCandidate(conf *CandidateConfig) (*Candidate, error) {
	var (
		id  string
		err error

		c = new(Candidate)
	)

	if conf == nil {
		return nil, errors.New("conf cannot be nil")
	}
	if conf.KVKey == "" {
		return nil, errors.New("conf.KVKey cannot be empty")
	}

	if c.ms, err = NewManagedSession(&conf.ManagedSessionConfig); err != nil {
		return nil, fmt.Errorf("error constructing ManagedSession: %s", err)
	}

	if conf.CandidateID == "" {
		if addr, err := LocalAddress(); err != nil {
			id = LazyRandomString(8)
		} else {
			id = addr
		}
	} else {
		id = conf.CandidateID
	}

	c.id = id
	c.notifierBase = newNotifierBase()
	c.kvKey = conf.KVKey
	c.consecutiveSessionErrors = new(uint64)
	*c.consecutiveSessionErrors = 0
	c.elected = new(bool)
	c.stop = make(chan chan struct{}, 1)

	c.ms.AttachNotificationHandler(fmt.Sprintf("candidate_%s", c.id), c.sessionUpdate)

	if conf.StartImmediately != nil {
		c.logf(true, "StartImmediately enabled")
		if err := c.Run(conf.StartImmediately); err != nil {
			return nil, fmt.Errorf("error occurred during auto run: %s", err)
		}
	}

	return c, nil
}

// CandidateID returns the configured identifier for this Candidate
func (c *Candidate) CandidateID() string {
	return c.id
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
func (c *Candidate) LeaderService(ctx context.Context) (*api.SessionEntry, *api.QueryMeta, error) {
	return c.ForeignLeaderService(ctx, "")
}

// Return the leader, assuming its CandidateID can be interpreted as an IP address
func (c *Candidate) LeaderIP(ctx context.Context) (net.IP, error) {
	return c.ForeignLeaderIP(ctx, "")
}

// ForeignLeaderIP will attempt to parse the body of the locked kv key to locate the current leader
func (c *Candidate) ForeignLeaderIP(ctx context.Context, dc string) (net.IP, error) {
	qo := c.ms.qo.WithContext(ctx)
	qo.Datacenter = dc
	kv, _, err := c.ms.client.KV().Get(c.kvKey, qo)
	if err != nil {
		return nil, err
	} else if kv == nil || len(kv.Value) == 0 {
		return nil, errors.New("no leader has been elected")
	}

	info := new(CandidateLeaderKVValue)
	if err = json.Unmarshal(kv.Value, info); err == nil && info.LeaderAddress != "" {
		if ip := net.ParseIP(info.LeaderAddress); ip != nil {
			return ip, nil
		}
	}
	return nil, fmt.Errorf("key \"%s\" had unexpected value \"%s\" for \"LeaderAddress\"", c.kvKey, string(kv.Value))
}

// ForeignLeaderService will attempt to locate the leader's session entry in a datacenter of your choosing
func (c *Candidate) ForeignLeaderService(ctx context.Context, dc string) (*api.SessionEntry, *api.QueryMeta, error) {
	var (
		kv  *api.KVPair
		se  *api.SessionEntry
		qm  *api.QueryMeta
		err error

		qo = c.ms.qo.WithContext(ctx)
	)

	qo.Datacenter = dc

	kv, qm, err = c.ms.client.KV().Get(c.kvKey, qo)
	if err != nil {
		return nil, qm, err
	}

	if nil == kv {
		return nil, qm, fmt.Errorf("kv \"%s\" not found in datacenter \"%s\"", c.kvKey, dc)
	}

	if kv.Session != "" {
		se, qm, err = c.ms.client.Session().Info(kv.Session, qo)
		if nil != se {
			return se, qm, nil
		}
	}

	return nil, qm, fmt.Errorf("kv \"%s\" has no session in datacenter \"%s\"", c.kvKey, dc)
}

// WaitUntil will wait for a candidate to be elected or until duration has passed
func (c *Candidate) WaitUntil(ctx context.Context) error {
	if !c.Running() {
		return fmt.Errorf("candidate %s is not in running", c.CandidateID())
	}

	var err error

	for i := 1; ; i++ {
		select {
		case <-ctx.Done():
			c.logf(false, "Context finished before locating leader: %s", ctx.Err())
			return ctx.Err()

		default:
			if _, _, err = c.LeaderService(ctx); nil == err {
				return nil
			}
			c.logf(false, "Attempt %d at locating leader service errored: %s", i, err)
		}

		time.Sleep(time.Second)
	}
}

// Wait will block until a leader has been elected, regardless of Candidate
func (c *Candidate) Wait() error {
	return c.WaitUntil(context.Background())
}

func (c *Candidate) State() CandidateState {
	c.mu.RLock()
	s := c.state
	c.mu.RUnlock()
	return s
}

func (c *Candidate) Running() bool {
	return c.State() == CandidateStateRunning
}

// Run will enter this candidate into the election pool.  If the candidate is already running this does nothing.
func (c *Candidate) Run(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state == CandidateStateRunning {
		return nil
	}

	if err := c.ms.Run(ctx); err != nil {
		return fmt.Errorf("session for candidate could not be started: %s", err)
	}

	c.logf(false, "Entering election pool...")

	// start up the lock maintainer
	go c.maintainLock(ctx)

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

	c.logf(false, "Leaving election pool...")

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

	if err := c.ms.Stop(); err != nil {
		c.logf(false, "Error stopping candidate session: %s", err)
	}

	c.mu.Unlock()

	c.logf(false, "Resigned")

	// notify watchers of updated state
	c.pushNotification(NotificationEventCandidateResigned)
}

// pushNotification constructs and then pushes a new notification to currently registered recipients based on the
// current state of the session.
//
// caller must hold at least read lock.
func (c *Candidate) pushNotification(ev NotificationEvent) {
	n := CandidateUpdate{
		Elected: c.Elected(),
		State:   c.State(),
	}
	c.sendNotification(NotificationSourceCandidate, ev, n)
}

func (c *Candidate) logf(debug bool, f string, v ...interface{}) {
	if c.logger != nil || debug && !c.dbg {
		return
	}
	c.logger.Printf(f, v...)
}

// acquire will attempt to do just that.  Caller must hold lock!
func (c *Candidate) acquire(ctx context.Context) (bool, error) {
	var (
		elected bool
		err     error
	)

	kvpValue := &CandidateLeaderKVValue{
		LeaderAddress: c.id,
	}

	kvp := &api.KVPair{
		Key:     c.kvKey,
		Session: c.ms.ID(),
	}

	kvp.Value, err = json.Marshal(kvpValue)
	if err != nil {
		c.logf(false, "Unable to marshal KV body: %s", err)
	}

	elected, _, err = c.ms.client.KV().Acquire(kvp, c.ms.wo.WithContext(ctx))
	return elected, err
}

// refreshLock is responsible for attempting to create / refresh the session lock on the kv
func (c *Candidate) refreshLock(ctx context.Context) {
	var (
		elected, updated bool
		err              error
	)

	c.mu.Lock()

	if c.ms.Running() {
		// if our session manager is still running
		if sid := c.ms.ID(); sid == "" {
			// this should only ever happen very early on in the election process
			elected = false
			updated = c.elected != nil && *c.elected != elected
			c.logf(true, "refreshLock() - ManagedSession does not exist, will try locking again in %d seconds...", int64(c.ms.RenewInterval().Seconds()))
		} else if elected, err = c.acquire(ctx); err != nil {
			// most likely hit due to transport error.
			updated = c.elected != nil && *c.elected != elected
			c.logf(true, "refreshLock() - Error attempting to acquire lock: %s", err)
		} else {
			// if c.elected is nil, indicating this is the initial election loop, or if the election state
			// changed mark update as true
			updated = c.elected == nil || *c.elected != elected
		}
	} else {
		c.logf(true, "refreshLock() - ManagedSession is in stopped state, attempting to restart...")
		elected = false
		updated = c.elected != nil && *c.elected != elected
		if err := c.ms.Run(ctx); err != nil {
			c.logf(false, "refreshLock() - Error restarting ManagedSession: %s", err)
		}
	}

	// if election state changed
	if updated {
		// update internal state
		*c.elected = elected

		if elected {
			c.logf(false, "We have won the election")
			c.pushNotification(NotificationEventCandidateElected)
		} else {
			c.logf(false, "We have lost the election")
			c.pushNotification(NotificationEventCandidateLostElection)
		}
	}

	if elected && !updated {
		c.pushNotification(NotificationEventCandidateRenew)
	}

	c.mu.Unlock()
}

// sessionUpdate is the receiver for the session update callback
func (c *Candidate) sessionUpdate(n Notification) {
	c.logf(true, "sessionUpdate() - Notification received: %v", n.Data)
	if !c.Running() {
		return
	}

	update, ok := n.Data.(ManagedSessionUpdate)
	if !ok {
		c.logf(false, "sessionUpdate() - Expected data to be of type %T, saw %T", ManagedSessionUpdate{}, n.Data)
		return
	}

	var refresh bool
	if update.Error != nil {
		// if there was an update either creating or renewing our session
		atomic.AddUint64(c.consecutiveSessionErrors, 1)
		c.logf(false, "sessionUpdate() - Error (%d in a row): %s", atomic.LoadUint64(c.consecutiveSessionErrors), update.Error)
		if update.State == ManagedSessionStateRunning && atomic.LoadUint64(c.consecutiveSessionErrors) > 2 {
			// if the session is still running but we've seen more than 2 errors, attempt a stop -> start cycle
			c.logf(false, "sessionUpdate() - 2 successive errors seen, stopping session")
			if err := c.ms.Stop(); err != nil {
				c.logf(false, "sessionUpdate() - Error stopping session: %s", err)
			}
			refresh = true
		}
		// do not modify elected state here unless we've breached the threshold.  could just be a temporary
		// issue
	} else if update.State == ManagedSessionStateStopped {
		// if somehow the session state became stopped (this should basically never happen...), do not attempt
		// to kickstart session here.  test if we need to update candidate state and notify watchers, then move
		// on.  next acquire tick will attempt to restart session.
		atomic.StoreUint64(c.consecutiveSessionErrors, 0)
		refresh = true
		c.logf(false, "sessionUpdate() - Stopped state seen: %#v", update)
	} else {
		// if we got a non-error / non-stopped update, there is nothing to do.
		atomic.StoreUint64(c.consecutiveSessionErrors, 0)
		c.logf(true, "sessionUpdate() - Received %#v", update)
	}

	if refresh {
		ctx, cancel := context.WithTimeout(context.Background(), c.ms.rttl)
		defer cancel()
		c.refreshLock(ctx)
	}
}

// maintainLock is responsible for triggering the routine that attempts to create / re-acquire the session kv lock
func (c *Candidate) maintainLock(ctx context.Context) {
	c.logf(true, "maintainLock() - Starting lock maintenance loop")
	var (
		drop chan struct{}

		renewInterval = c.ms.RenewInterval()
		renewTimer    = time.NewTimer(renewInterval)
	)

	defer func() {
		c.logf(false, "maintainLock() - Exiting lock maintenance loop")
		renewTimer.Stop()
		c.Resign()
		if drop != nil {
			drop <- struct{}{}
		}
		c.pushNotification(NotificationEventCandidateStopped)
	}()

	c.pushNotification(NotificationEventCandidateRunning)

	for {
		select {
		case tick := <-renewTimer.C:
			c.logf(true, "maintainLock() - renewTimer tick (%s)", tick)
			c.refreshLock(ctx)
			renewTimer.Reset(renewInterval)

		case <-ctx.Done():
			c.logf(false, "maintainLock() - context completed: %s", ctx.Err())
			return

		case drop = <-c.stop:
			c.logf(false, "maintainLock() - stop hit")
			return
		}
	}
}
