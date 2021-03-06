package consultant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/consul/api"
)

type CandidateState uint8

const (
	// 0x10 - 0x1f
	CandidateStateResigned   CandidateState = 0x10
	CandidateStateRunning    CandidateState = 0x11
	CandidateStateShutdowned CandidateState = 0x12
)

func (s CandidateState) String() string {
	switch s {
	case CandidateStateResigned:
		return "resigned"
	case CandidateStateRunning:
		return "running"
	case CandidateStateShutdowned:
		return "shutdowned"

	default:
		return "UNKNOWN"
	}
}

// CandidateLeaderKVValueProvider is called whenever a running Candidate attempts to acquire the lock on the defined kv
// key.  The resulting []byte is used as the data payload, and may be whatever you wish.
//
// A call to this func does NOT mean that the provided candidate IS the leader, it means that it is ATTEMPTING TO
// BECOME the leader.
type CandidateLeaderKVValueProvider func(*Candidate) ([]byte, error)

// CandidateDefaultLeaderKVValue is the body of the acquired LeaderKV when no CandidateLeaderKVValueProvider is provided
// during candidate configuration
type CandidateDefaultLeaderKVValue struct {
	LeaderID  string `json:"leader_id"`
	SessionID string `json:"session_id"`
}

// CandidateDefaultLeaderKVValueProvider is the default data provider used when none is configured for a given candidate
func CandidateDefaultLeaderKVValueProvider(c *Candidate) ([]byte, error) {
	v := new(CandidateDefaultLeaderKVValue)
	v.LeaderID = c.ID()
	v.SessionID = c.ms.ID()
	return json.Marshal(v)
}

// CandidateUpdate is the value of .Data in all Notification pushes from a Candidate
type CandidateUpdate struct {
	// ID will be the ID of the Candidate pushing this update
	ID string `json:"id"`
	// Elected tracks whether this specific candidate has been elected
	Elected bool `json:"elected"`
	// State tracks the current state of this candidate
	State CandidateState `json:"state"`
	// Error will be defined if there an error associated with the notification
	Error error `json:"error"`
}

// CandidateConfig describes a Candidate
type CandidateConfig struct {
	ManagedSessionConfig

	// KVKey [required]
	//
	// Must be the key to attempt to acquire a session lock on.  This key must be considered ephemeral, and not contain
	// anything you don't want overwritten / destroyed.
	KVKey string

	// KVDataProvider [optional]
	//
	// Optionally provide a callback func that returns a []byte to be used as the data value when a running Candidate
	// acquires the lock (i.e. is "elected").  Calls to this method MUST NOT be taken as a sign of the provided
	// candidate having been elected.  It ONLY indicates that the candidate is ATTEMPTING to be elected.
	KVDataProvider CandidateLeaderKVValueProvider

	// ID [suggested]
	//
	// Should be a unique identifier for this specific Candidate that makes sense within the scope of your
	// implementation. If left blank it will attempt to use the local IP address, otherwise a random string will be
	// generated.  This is a way to identify which Candidate is holding the lock.
	ID string

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
// to a specific LeaderKV key.  This can then be used to facilitate "leader election" by way of the "leader" being the
// Candidate who's session is locking the target key.
type Candidate struct {
	*notifierBase
	mu sync.RWMutex

	ms *ManagedSession

	id              string
	kvKey           string
	kvValueProvider CandidateLeaderKVValueProvider
	elected         *bool
	state           CandidateState

	dbg bool
	log Logger

	consecutiveSessionErrors *uint64
	stop                     chan chan error
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

	c.log = conf.Logger
	c.dbg = conf.Debug

	if conf.ID == "" {
		if addr, err := LocalAddress(); err != nil {
			id = LazyRandomString(8)
			c.logf(false, "No ID defined in config and error returned from LocalAddress (%s).  Setting ID to %q", err, id)
		} else {
			id = addr
			c.logf(true, "No ID defined, setting ID to %q", id)
		}
	} else {
		id = conf.ID
	}

	c.id = id
	c.notifierBase = newNotifierBase(c.log, c.dbg)
	c.kvKey = conf.KVKey
	c.consecutiveSessionErrors = new(uint64)
	*c.consecutiveSessionErrors = 0
	c.elected = new(bool)
	c.stop = make(chan chan error, 1)

	if conf.KVDataProvider == nil {
		c.kvValueProvider = CandidateDefaultLeaderKVValueProvider
	} else {
		c.kvValueProvider = conf.KVDataProvider
	}

	c.ms.AttachNotificationHandler(fmt.Sprintf("candidate_%s", c.id), c.sessionUpdate)

	if conf.StartImmediately {
		c.logf(true, "StartImmediately enabled")
		if err := c.Run(); err != nil {
			return nil, fmt.Errorf("error occurred during auto run: %s", err)
		}
	}

	return c, nil
}

// ID returns the configured identifier for this Candidate
func (c *Candidate) ID() string {
	return c.id
}

// Elected will return true if this candidate's session is "locking" the kv
func (c *Candidate) Elected() bool {
	c.mu.RLock()
	el := c.elected != nil && *c.elected
	c.mu.RUnlock()
	return el
}

// Session returns the underlying ManagedSession instance used by this Candidate
func (c *Candidate) Session() *ManagedSession {
	return c.ms
}

// LeaderKV attempts to return the LeaderKV being used to control leader election in the local datacenter
func (c *Candidate) LeaderKV(ctx context.Context) (*api.KVPair, *api.QueryMeta, error) {
	return c.ForeignLeaderKV(ctx, "")
}

// ForeignLeaderKV attempts to return the LeaderKV being used to control leader election in the specified datacenter
func (c *Candidate) ForeignLeaderKV(ctx context.Context, datacenter string) (*api.KVPair, *api.QueryMeta, error) {
	var (
		kv  *api.KVPair
		qm  *api.QueryMeta
		err error

		qo = c.ms.qo.WithContext(ctx)
	)

	qo.Datacenter = datacenter

	if kv, qm, err = c.ms.client.KV().Get(c.kvKey, qo); err != nil {
		return nil, qm, err
	}

	if nil == kv {
		return nil, qm, fmt.Errorf("kv \"%s\" not found in datacenter \"%s\"", c.kvKey, datacenter)
	}

	return kv, qm, nil
}

// LeaderSession will attempt to locate the leader's session entry in your local agent's datacenter
func (c *Candidate) LeaderSession(ctx context.Context) (*api.SessionEntry, *api.QueryMeta, error) {
	return c.ForeignLeaderSession(ctx, "")
}

// ForeignLeaderSession will attempt to locate the leader's session entry in a datacenter of your choosing
func (c *Candidate) ForeignLeaderSession(ctx context.Context, datacenter string) (*api.SessionEntry, *api.QueryMeta, error) {
	var (
		kv  *api.KVPair
		se  *api.SessionEntry
		qm  *api.QueryMeta
		qo  *api.QueryOptions
		err error
	)

	if kv, qm, err = c.ForeignLeaderKV(ctx, datacenter); err != nil {
		return nil, qm, err
	}

	if kv.Session != "" {
		qo = c.ms.qo.WithContext(ctx)
		qo.Datacenter = datacenter
		se, qm, err = c.ms.client.Session().Info(kv.Session, qo)
		if nil != se {
			return se, qm, nil
		}
	}

	return nil, qm, fmt.Errorf("kv \"%s\" has no session in datacenter \"%s\"", c.kvKey, datacenter)
}

// WaitUntil will wait for a candidate to be elected or until duration has passed
func (c *Candidate) WaitUntil(ctx context.Context) error {
	for i := 1; ; i++ {
		if !c.Running() {
			return fmt.Errorf("candidate %s is not in running", c.ID())
		}
		select {
		case <-ctx.Done():
			c.logf(false, "Context finished before locating leader: %s", ctx.Err())
			return ctx.Err()

		default:
			if _, _, err := c.LeaderSession(ctx); nil == err {
				return nil
			} else {
				c.logf(false, "Attempt %d at locating leader service errored: %s", i, err)
			}
		}

		time.Sleep(time.Second)
	}
}

// Wait will block until a leader has been elected, regardless of Candidate
func (c *Candidate) Wait() error {
	return c.WaitUntil(context.Background())
}

// WaitUntilNotify accepts a channel that will have the end result of .WaitUntil() pushed onto it.
func (c *Candidate) WaitUntilNotify(ctx context.Context, ch chan<- error) {
	ch <- c.WaitUntil(ctx)
}

// WaitNotify accepts a channel that will have the end result of .Wait() pushed onto it
func (c *Candidate) WaitNotify(ch chan<- error) {
	ch <- c.Wait()
}

// State returns the current state of this Candidate
func (c *Candidate) State() CandidateState {
	c.mu.RLock()
	s := c.state
	c.mu.RUnlock()
	return s
}

// Running returns true if the current state of the Candidate is running
func (c *Candidate) Running() bool {
	return c.State() == CandidateStateRunning
}

// Resigned returns true if the current state of the Candidate is resigned
func (c *Candidate) Resigned() bool {
	return c.State() == CandidateStateResigned
}

// Shutdowned returns true if the current state of the candidate is shutdowned
func (c *Candidate) Shutdowned() bool {
	return c.State() == CandidateStateShutdowned
}

// Run will enter this candidate into the election pool.  If the candidate is already running this does nothing.
func (c *Candidate) Run() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state == CandidateStateRunning {
		return nil
	}

	if c.state == CandidateStateShutdowned {
		return errors.New("candidate is shutdowned")
	}

	c.setState(CandidateStateRunning)

	c.logf(false, "Run() - Entering election pool...")
	c.logf(true, "Run() - Starting up managed session...")

	if err := c.ms.Run(); err != nil {
		return fmt.Errorf("session for candidate could not be started: %s", err)
	}

	c.logf(true, "Run() - Managed session started with ID %q", c.ms.ID())

	// start up the lock maintainer
	go c.maintainLock()

	return c.refreshLock()
}

// Resign will remove this candidate from the election pool
func (c *Candidate) Resign() error {
	c.mu.Lock()
	if c.state == CandidateStateResigned {
		c.mu.Unlock()
		c.logf(true, "Resign() called but we're already resigned")
		return nil
	}
	if c.state == CandidateStateShutdowned {
		c.mu.Unlock()
		c.logf(false, "Resign() called but we're shutdowned")
		return nil
	}

	c.setState(CandidateStateResigned)

	c.mu.Unlock()

	return c.waitForResign()
}

// Shutdown will remove this candidate from the election pool and render it defunct
func (c *Candidate) Shutdown() error {
	c.mu.Lock()
	if c.state == CandidateStateShutdowned {
		c.mu.Unlock()
		c.logf(true, "Shutdown() called but we're already shutdowned")
		return nil
	}

	var (
		err error

		// do we need to perform stop actions?
		requiresStop = c.state == CandidateStateRunning
	)

	// set state to shutdowned
	c.setState(CandidateStateShutdowned)

	// unlock
	c.mu.Unlock()

	if requiresStop {
		// wait for resignation
		err = c.waitForResign()
	}

	// detach all notifiers
	c.DetachAllNotificationRecipients(true)

	// close stop chan
	c.mu.Lock()
	close(c.stop)
	c.mu.Unlock()

	// return any error seen during resignation
	return err
}

// buildUpdate constructs a notification update type
//
// caller must hold lock
func (c *Candidate) buildUpdate(err error) CandidateUpdate {
	return CandidateUpdate{
		ID:      c.id,
		Elected: c.elected != nil && *c.elected,
		State:   c.state,
		Error:   err,
	}
}

// pushNotification constructs and then pushes a new notification to currently registered recipients based on the
// current state of the candidate.
func (c *Candidate) pushNotification(ev NotificationEvent, up CandidateUpdate) {
	c.sendNotification(NotificationSourceCandidate, ev, up)
}

func (c *Candidate) logf(debug bool, f string, v ...interface{}) {
	if c.log == nil || (debug && !c.dbg) {
		return
	}
	c.log.Printf(f, v...)
}

func (c *Candidate) waitForResign() error {
	drop := make(chan error, 1)
	c.stop <- drop
	err := <-drop
	close(drop)
	return err
}

// setState updates the internal state value and pushes a notification of change
//
// caller must hold full lock
func (c *Candidate) setState(state CandidateState) {
	var ev NotificationEvent

	if c.state == state {
		return
	}

	switch state {
	case CandidateStateRunning:
		ev = NotificationEventCandidateRunning
	case CandidateStateResigned:
		ev = NotificationEventCandidateResigned
	case CandidateStateShutdowned:
		ev = NotificationEventCandidateShutdowned

	default:
		panic(fmt.Sprintf("unkonwn state %d (%[1]s) seen", state))
	}

	c.state = state

	c.pushNotification(ev, c.buildUpdate(nil))
}

// acquire will attempt to do just that.  Caller must hold lock!
func (c *Candidate) acquire() (bool, error) {
	var (
		elected bool
		err     error
	)

	kvp := &api.KVPair{
		Key:     c.kvKey,
		Session: c.ms.ID(),
	}

	kvp.Value, err = c.kvValueProvider(c)
	if err != nil {
		c.logf(false, "Unable to marshal LeaderKV body: %s", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.ms.requestTTL)
	defer cancel()
	elected, _, err = c.ms.client.KV().Acquire(kvp, c.ms.wo.WithContext(ctx))
	return elected, err
}

// refreshLock is responsible for attempting to create / refresh the session lock on the kv
func (c *Candidate) refreshLock() error {
	var (
		elected, updated bool
		err              error
	)

	if c.ms.Running() {
		// if our session manager is still running
		if sid := c.ms.ID(); sid == "" {
			// this should only ever happen very early on in the election process
			elected = false
			updated = c.elected != nil && *c.elected != elected
			c.logf(false, "refreshLock() - ManagedSession does not exist, will try locking again in %d seconds...", int64(c.ms.RenewInterval().Seconds()))
		} else if elected, err = c.acquire(); err != nil {
			// most likely hit due to transport error.
			updated = c.elected != nil && *c.elected != elected
			c.logf(false, "refreshLock() - Error attempting to acquire lock: %s", err)
		} else {
			// if c.elected is nil, indicating this is the initial election loop, or if the election state
			// changed mark update as true
			updated = c.elected == nil || *c.elected != elected
		}
	} else {
		c.logf(false, "refreshLock() - ManagedSession is in stopped state, attempting to restart...")
		elected = false
		updated = c.elected != nil && *c.elected != elected
		if err := c.ms.Run(); err != nil {
			c.logf(false, "refreshLock() - Error restarting ManagedSession: %s", err)
		}
	}

	// if election state changed
	if updated {
		// update internal state
		*c.elected = elected
	}

	up := c.buildUpdate(err)

	// if our state changed, notify accordingly
	if updated {
		if elected {
			c.logf(false, "refreshLock() - We have won the election")
			c.pushNotification(NotificationEventCandidateElected, up)
		} else {
			c.logf(false, "refreshLock() - We have lost the election")
			c.pushNotification(NotificationEventCandidateLostElection, up)
		}
	} else if elected {
		// if we were already elected, push "renewed" notification
		c.pushNotification(NotificationEventCandidateRenew, up)
	}

	return err
}

// sessionUpdate is the receiver for the session update callback
func (c *Candidate) sessionUpdate(n Notification) {
	c.logf(true, "sessionUpdate() - Notification received: %v", n.Data)
	if !c.Running() {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

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
		c.logf(false, "sessionUpdate() - refreshing lock")
		_ = c.refreshLock()
	}
}

func (c *Candidate) doStop() error {
	var err error

	// only update elected state if we were ever elected in the first place.
	if c.elected != nil {
		*c.elected = false
	}

	c.logf(true, "doStop() - Deleting key %q", c.kvKey)
	ctx, cancel := context.WithTimeout(context.Background(), c.ms.requestTTL)
	defer cancel()
	if _, err := c.ms.client.KV().Delete(c.kvKey, c.ms.wo.WithContext(ctx)); err != nil {
		c.logf(false, "doStop() - Error deleting key %q: %s", c.kvKey, err)
	}

	c.logf(true, "doStop() - Stopping managed session...")
	if err = c.ms.Stop(); err != nil {
		c.logf(false, "doStop() - Error stopping candidate managed session (%s): %s", c.ms.ID(), err)
	} else {
		c.logf(true, "doStop() - Managed session stopped")
	}

	return err
}

// maintainLock is responsible for triggering the routine that attempts to create / re-acquire the session kv lock
func (c *Candidate) maintainLock() {
	c.logf(true, "maintainLock() - Starting lock maintenance loop")
	var (
		renewInterval = c.ms.RenewInterval()
		renewTimer    = time.NewTimer(renewInterval)
	)

	for {
		select {
		case tick := <-renewTimer.C:
			c.logf(true, "maintainLock() - renewTimer tick (%s)", tick)
			c.mu.Lock()
			_ = c.refreshLock()
			c.mu.Unlock()
			renewTimer.Reset(renewInterval)

		case drop := <-c.stop:
			c.logf(false, "maintainLock() - stop called")
			c.mu.Lock()
			err := c.doStop()
			c.mu.Unlock()
			drop <- err
			if !renewTimer.Stop() {
				<-renewTimer.C
			}
			return
		}
	}
}
