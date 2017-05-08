package consultant

import (
	"fmt"
	"github.com/hashicorp/consul/api"
	"github.com/renstrom/shortuuid"
	"math/rand"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	validCandidateIDRegex = `[a-zA-Z0-9:._-]+` // only allow certain characters in an ID
	CandidateMaxWait      = 10                 // specifies the maximum wait after failed lock checks
)

var validCandidateIDTest = regexp.MustCompile(validCandidateIDRegex)
var candidateIDErrMsg = fmt.Sprintf("Candidate ID must obey \"%s\"", validCandidateIDRegex)

type Candidate struct {
	client *api.Client

	lock *sync.RWMutex

	id           string
	logSlug      string
	logSlugSlice []interface{}

	kv           *api.KVPair
	sessionTTL   time.Duration
	sessionID    string
	sessionEntry *api.SessionEntry

	wait    *sync.WaitGroup
	leader  bool
	closing bool

	update map[string]chan bool
}

// NewCandidate creates a new Candidate
//
// - "client" must be a valid api client
//
// - "id" should be an implementation-relevant unique identifier
//
// - "key" must be the full path to a KV, it will be created if it doesn't already exist
//
// - "ttl" is the duration to set on the kv session ttl, will default to 30s if not specified
func NewCandidate(client *api.Client, id, key, ttl string) (*Candidate, error) {
	var err error
	var ttlSeconds float64

	id = strings.TrimSpace(id)

	if !validCandidateIDTest.MatchString(id) {
		return nil, getCandidateError(CandidateErrorInvalidID)
	}

	c := &Candidate{
		client: client,
		id:     id,
		lock:   new(sync.RWMutex),
		wait:   new(sync.WaitGroup),
		update: make(map[string]chan bool),
	}

	// create some slugs for log output
	c.logSlug = fmt.Sprintf("[candidate-%s]", id)
	c.logSlugSlice = []interface{}{c.logSlug}

	// begin session entry construction
	c.sessionEntry = &api.SessionEntry{
		Name:     fmt.Sprintf("leader-session-%s-%s", id, shortuuid.New()),
		Behavior: api.SessionBehaviorDelete,
	}

	if debug {
		c.logPrintf("Session name \"%s\"", c.sessionEntry.Name)
	}

	// if ttl empty, default to 30 seconds
	if "" == ttl {
		ttl = "30s"
	}

	// validate ttl
	c.sessionTTL, err = time.ParseDuration(ttl)
	if nil != err {
		c.logPrintf("Unable to parse provided TTL: %v", err)
		return nil, getCandidateError(CandidateErrorInvalidTTL)
	}

	// stay within the limits...
	ttlSeconds = c.sessionTTL.Seconds()
	if 10 > ttlSeconds {
		c.sessionEntry.TTL = "10s"
	} else if 86400 < ttlSeconds {
		c.sessionEntry.TTL = "86400s"
	} else {
		c.sessionEntry.TTL = ttl
	}

	if debug {
		c.logPrintf("Setting TTL to \"%s\"", c.sessionEntry.TTL)
	}

	// create new session
	c.sessionID, _, err = c.client.Session().Create(c.sessionEntry, nil)
	if nil != err {
		return nil, err
	}

	if debug {
		c.logPrintf("Session created with id \"%s\"", c.sessionID)
	}

	// store session id
	c.sessionEntry.ID = c.sessionID

	c.kv = &api.KVPair{
		Key:     key,
		Session: c.sessionID,
	}

	go c.sessionKeepAlive()
	go c.lockRunner()

	return c, nil
}

// ID returns the unique identifier given at construct
func (c *Candidate) ID() string {
	return c.id
}

// SessionID is the name of this candidate's session
func (c *Candidate) SessionID() string {
	return c.sessionID
}

// SessionTTL returns the parsed TTL
func (c *Candidate) SessionTTL() time.Duration {
	return c.sessionTTL
}

// Elected will return true if this candidate's session is "locking" the kv
func (c *Candidate) Elected() bool {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.leader
}

// Resign makes this candidate defunct.
func (c *Candidate) Resign() {
	c.lock.Lock()
	c.leader = false
	c.closing = true
	c.lock.Unlock()

	if debug {
		c.logPrintln("Resigning candidacy ... waiting for routines to exit ...")
	}

	c.wait.Wait()

	c.logPrintln("Candidacy resigned.  We're no longer in the running")
}

// Leader will attempt to locate the leader's session entry in your local agent's datacenter
func (c *Candidate) Leader() (*api.SessionEntry, error) {
	return c.ForeignLeader("")
}

// ForeignLeader will attempt to locate the leader's session entry in a datacenter of your choosing
func (c *Candidate) ForeignLeader(dc string) (*api.SessionEntry, error) {
	var kv *api.KVPair
	var se *api.SessionEntry
	var err error

	qo := &api.QueryOptions{}

	if "" != dc {
		qo.Datacenter = dc
	}

	kv, _, err = c.client.KV().Get(c.kv.Key, qo)
	if nil != err {
		return nil, err
	}

	if nil == kv {
		c.logPrintf("Unable to locate kv \"%s\" in datacenter \"%s\"", c.kv.Key, dc)
		return nil, getCandidateError(CandidateErrorKeyNotFound)
	}

	if "" != kv.Session {
		se, _, err = c.client.Session().Info(kv.Session, qo)
		if nil != se {
			return se, nil
		}
	}

	c.logPrintf("No session associated with kv \"%s\" in datacenter \"%s\"", c.kv.Key, dc)
	return nil, getCandidateError(CandidateErrorNoSession)
}

// Wait will block until a leader has been elected, regardless of candidate.
func (c *Candidate) Wait() {
	var err error

	for {
		// attempt to locate current leader
		if _, err = c.Leader(); nil == err {
			break
		}

		// if session empty, assume no leader elected yet and try again
		time.Sleep(time.Second * 1)
	}
}

// Register returns a channel for updates in leader status -
// only one message per candidate instance will be sent
func (c *Candidate) RegisterUpdate(id string) (string, chan bool) {
	c.lock.Lock()
	defer c.lock.Unlock()
	if id == "" {
		id = randToken()
	}
	cup, ok := c.update[id]
	if ok {
		return id, cup
	} else {
		cup = make(chan bool, 1)
		c.update[id] = cup
		return id, cup
	}
}

func (c *Candidate) DeregisterUpdate(id string) {
	c.lock.Lock()
	defer c.lock.Unlock()
	_, ok := c.update[id]
	if ok {
		delete(c.update, id)
	}
}

// updateLeader is a thread safe update of leader status
func (c *Candidate) updateLeader(v bool) {
	c.lock.Lock()
	defer c.lock.Unlock()

	// Send out updates if leader status changed
	if v != c.leader {
		for _, cup := range c.update {
			cup <- v
		}
	}

	// Update the leader flag
	c.leader = v
}

func randToken() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// lockRunner runs the lock
func (c *Candidate) lockRunner() {
	var se *api.SessionEntry // session entry
	var kv *api.KVPair       // retrieved key
	var qm *api.QueryMeta    // metadata
	var qo *api.QueryOptions // options
	var err error            // error holder
	var ok bool              // lock status
	var checkWait int        // fail retry

	// build options
	qo = new(api.QueryOptions)
	qo.WaitIndex = uint64(0)
	qo.WaitTime = c.sessionTTL

	// increment wait group
	c.wait.Add(1)

	// cleanup on exit
	defer c.wait.Done()

	// main loop
	for {
		// attempt to get the key
		if kv, qm, err = c.client.KV().Get(c.kv.Key, qo); err != nil {
			// log warning
			c.logPrintf("lockRunner() error checking lock: %s", err)
			// increment counter up to maximum value
			if checkWait < CandidateMaxWait {
				checkWait++
			}
			// log sleep
			if debug {
				c.logPrintf("lockRunner() sleeping for %d seconds before retry ...", checkWait)
			}
			// sleep before retry
			time.Sleep(time.Duration(checkWait) * time.Second)
			/// next interation
			continue
		}

		// reset wait on success
		checkWait = 0

		// check closing
		if c.closing {
			if debug {
				c.logPrintln("lockRunner() exiting")
			}
			return
		}

		// update index
		qo.WaitIndex = qm.LastIndex

		// check kv
		if kv != nil {
			if kv.Session == c.sessionID {
				// we are the leader
				c.updateLeader(true)
				continue
			}

			// still going ... check session
			if kv.Session != "" {
				// lock (probably) held by someone else...try to find out who it is
				se, _, err = c.client.Session().Info(kv.Session, nil)
				// check error
				if err != nil {
					// failed to get session - log error
					c.logPrintf("lockRuner() error fetching session: %s", err)
					// renew/rebuild session
					c.sessionValidate()
					// wait for next iteration
					continue
				}
				// check returned session entry
				if se == nil {
					// nil session entry - nil kv and attempt to get the lock below
					kv = nil
				}
			} else {
				// missing session - nil kv and attempt to get the lock below
				kv = nil
			}
		}

		// not the leader
		c.updateLeader(false)

		// check for nil key
		if kv == nil {
			if debug {
				c.logPrintln("lockRunner() nil lock or empty session detected - attempting to get lock ...")
			}
			// attempt to get the lock and check for error
			if ok, _, err = c.client.KV().Acquire(c.kv, nil); err != nil {
				c.logPrintf("lockRunner() error failed to aquire lock: %s", err)
				// renew/rebuild session
				c.sessionValidate()
				// wait for next iteration
				continue
			}
			// we might have the lock
			if ok && err == nil {
				c.logPrintf("lockRunner() acquired lock with session %s", c.sessionID)
				// yep .. we're the leader
				c.updateLeader(true)
				continue
			}
		}
	}
}

// sessionValidate renews/recreates the session as needed
func (c *Candidate) sessionValidate() {
	var se *api.SessionEntry // session object
	var sid string           // temp session id
	var err error            // error holder

	// attempt to renew session
	if se, _, err = c.client.Session().Renew(c.sessionID, nil); err != nil || se == nil {
		// check error
		if err != nil {
			// log error
			c.logPrintf("sessionValidate() failed to renew session: %s", err)
			// destroy sesion
			c.client.Session().Destroy(c.sessionID, nil)
		}
		// check session
		if se == nil {
			// log error
			c.logPrintln("sessionValidate() failed to renew session: not found")
		}
		// recreate the session
		if sid, _, err = c.client.Session().Create(c.sessionEntry, nil); err != nil {
			c.logPrintf("sessionValidate() failed to rebuild session: %s", err)
			return
		}
		// update session and lock pair
		c.lock.Lock()
		c.sessionID = sid
		c.kv.Session = sid
		c.lock.Unlock()
		// log session rebuild
		if debug {
			c.logPrintf("sessionValidate() registered new session %s", sid)
		}
		// all done
		return
	}
	// renew okay
	// log.Debug("sessionValidate() renewed session: %s", c.sid)
}

// sessionKeepAlive keeps session and ttl check alive
func (c *Candidate) sessionKeepAlive() {
	var sleepDuration time.Duration // sleep duration
	var sleepTicker *time.Ticker    // sleep timer
	var err error                   // error holder

	// increment wait group
	c.wait.Add(1)

	// cleanup on exit
	defer c.wait.Done()

	// ensure sleep always at least one second
	if sleepDuration = c.sessionTTL / 2; sleepDuration < time.Second {
		sleepDuration = time.Second
	}

	// init ticker channel
	sleepTicker = time.NewTicker(sleepDuration)

	// loop every "sleep" seconds
	for range sleepTicker.C {
		// check closing
		if c.closing {
			if debug {
				c.logPrintln("sessionKeepAlive() exiting")
			}
			// destroy session
			if _, err = c.client.Session().Destroy(c.sessionID, nil); err != nil {
				c.logPrintf("sessionKeepAlive() failed to destroy session (%s) %s",
					c.sessionID,
					err)
			}
			// stop ticker and exit
			sleepTicker.Stop()
			return
		}
		// renew/rebuild session
		c.sessionValidate()
	}

	// shouldn't ever happen
	if !c.closing {
		c.logPrintln("sessionKeepAlive() exiting unexpectedly")
	}
}

func (c *Candidate) logPrintf(format string, v ...interface{}) {
	log.Printf(fmt.Sprintf("%s %s", c.logSlug, format), v...)
}

func (c *Candidate) logPrint(v ...interface{}) {
	log.Print(append(c.logSlugSlice, v...)...)
}

func (c *Candidate) logPrintln(v ...interface{}) {
	log.Println(append(c.logSlugSlice, v...)...)
}

func (c *Candidate) logFatalf(format string, v ...interface{}) {
	log.Fatalf(fmt.Sprintf("%s %s", c.logSlug, format), v...)
}

func (c *Candidate) logFatal(v ...interface{}) {
	log.Fatal(append(c.logSlugSlice, v...)...)
}

func (c *Candidate) logFatalln(v ...interface{}) {
	log.Fatalln(append(c.logSlugSlice, v...)...)
}

func (c *Candidate) logPanicf(format string, v ...interface{}) {
	log.Panicf(fmt.Sprintf("%s %s", c.logSlug, format), v...)
}

func (c *Candidate) logPanic(v ...interface{}) {
	log.Panic(append(c.logSlugSlice, v...)...)
}

func (c *Candidate) logPanicln(v ...interface{}) {
	log.Panicln(append(c.logSlugSlice, v...)...)
}
