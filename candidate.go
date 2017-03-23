package consultant

import (
	"fmt"
	"github.com/hashicorp/consul/api"
	"github.com/renstrom/shortuuid"
	"regexp"
	"strings"
	"sync"
	"time"
)

// only allow certain characters in an ID
const validIDRegex = `[a-zA-Z0-9:._-]+`

var validIDTest = regexp.MustCompile(validIDRegex)

type Candidate struct {
	client *api.Client

	lock *sync.RWMutex

	id string

	kv           *api.KVPair
	sessionTTL   time.Duration
	sessionID    string
	sessionEntry *api.SessionEntry

	wait    *sync.WaitGroup
	leader  bool
	closing bool

	update map[string]chan bool
}

// New creates a new Candidate
//
// - "id" should be an implementation-relevant unique identifier
//
// - "key" must be the full path to a KV, it will be created if it doesn't already exist
//
// - "ttl" is the duration to set on the kv session ttl, will default to 30s if not specified
//
// - "client" must be a valid api client
func New(id, key, ttl string, client *api.Client) (*Candidate, error) {
	var err error
	var ttlSeconds float64

	id = strings.TrimSpace(id)

	if !validIDTest.MatchString(id) {
		return nil, fmt.Errorf("ID value must obey \"%s\", \"%s\" does not.", validIDRegex, id)
	}

	c := &Candidate{
		client: client,
		id:     id,
		lock:   new(sync.RWMutex),
		wait:   new(sync.WaitGroup),
		update: make(map[string]chan bool),
	}

	// begin session entry construction
	c.sessionEntry = &api.SessionEntry{
		Name:     fmt.Sprintf("leader-session-%s-%s", id, shortuuid.New()),
		Behavior: api.SessionBehaviorDelete,
	}

	if debug {
		logPrintf(c, "Session name \"%s\"", c.sessionEntry.Name)
	}

	// if ttl empty, default to 30 seconds
	if "" == ttl {
		ttl = "30s"
	}

	// validate ttl
	c.sessionTTL, err = time.ParseDuration(ttl)
	if nil != err {
		return nil, err
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
		logPrintf(c, "Setting TTL to \"%d\" seconds", int64(ttlSeconds))
	}

	// create new session
	c.sessionID, _, err = c.client.Session().Create(c.sessionEntry, nil)
	if nil != err {
		return nil, err
	}

	if debug {
		logPrintf(c, "Session created with id \"%s\"", c.sessionID)
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
		logPrintln(c, "Resigning candidacy ... waiting for routines to exit ...")
	}

	c.wait.Wait()

	logPrintln(c, "Candidacy resigned.  We're no longer in the running")
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
		return nil, fmt.Errorf("Unable to locate kv \"%s\" in datacenter \"%s\"", c.kv.Key, dc)
	}

	if "" != kv.Session {
		se, _, err = c.client.Session().Info(kv.Session, qo)
		if nil != se {
			return se, nil
		}
	}

	return nil, fmt.Errorf("No session associated with kv \"%s\" in datacenter \"%s\"", c.kv.Key, dc)
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
