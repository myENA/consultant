package candidate

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hashicorp/consul/api"
	"github.com/myENA/consultant/candidate/session"
	"github.com/myENA/consultant/log"
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
	validCandidateIDRegex = `[a-zA-Z0-9:._-]+` // only allow certain characters in an ID
)

var (
	validCandidateIDTest = regexp.MustCompile(validCandidateIDRegex)
	InvalidCandidateID   = fmt.Errorf("vandidate ID must obey \"%s\"", validCandidateIDRegex)
)

type Candidate struct {
	mu  sync.Mutex
	log log.DebugLogger

	client *api.Client

	id       string
	session  *session.Session
	watchers *watchers

	kvKey string
	ttl   time.Duration

	elected bool
	state   State
	resign  chan chan struct{}
}

// New creates a new Candidate
//
// - "client" must be a valid api client
//
// - "candidateID" should be an implementation-relevant unique identifier for this candidate
//
// - "kvKey" must be the full path to a KV, it will be created if it doesn't already exist
//
// - "sessionTTL" is the duration to set on the kv session ttl, will default to 30s if not specified
func New(client *api.Client, candidateID, kvKey, sessionTTL string) (*Candidate, error) {
	var err error

	if client == nil {
		client, err = api.NewClient(api.DefaultConfig())
		if err != nil {
			return nil, err
		}
	}

	candidateID = strings.TrimSpace(candidateID)
	if candidateID == "" || !validCandidateIDTest.MatchString(candidateID) {
		return nil, InvalidCandidateID
	}

	if kvKey == "" {
		return nil, errors.New("key cannot be empty")
	}

	c := &Candidate{
		log:      log.New(fmt.Sprintf("candidate-%s", candidateID)),
		client:   client,
		id:       candidateID,
		watchers: newWatchers(),
		kvKey:    kvKey,
		resign:   make(chan chan struct{}, 1),
	}

	c.session, err = session.New(c.log, client, c.id, sessionTTL, c.sessionUpdate)
	if err != nil {
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
	el := c.elected
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

// Return the leader of a foreign datacenter, assuming its ID can be interpreted as an IP address
func (c *Candidate) ForeignLeaderIP(dc string) (net.IP, error) {
	leaderSession, err := c.ForeignLeaderService(dc)
	if nil != err {
		return nil, fmt.Errorf("leaderAddress() Error getting leader address: %s", err)
	}

	// parse session name
	parts, err := ParseSessionName(leaderSession.Name)
	if nil != err {
		return nil, fmt.Errorf("leaderAddress() Unable to parse leader session name: %s", err)
	}

	// attempt to validate value
	ip := net.ParseIP(parts.ID)
	if nil == ip {
		return nil, fmt.Errorf("leaderAddress() Unable to parse IP address from \"%s\"", parts.ID)
	}

	return ip, nil
}

// ForeignLeaderService will attempt to locate the leader's session entry in a datacenter of your choosing
func (c *Candidate) ForeignLeaderService(dc string) (*api.SessionEntry, error) {
	var kv *api.KVPair
	var se *api.SessionEntry
	var err error

	qo := &api.QueryOptions{}

	if "" != dc {
		qo.Datacenter = dc
	}

	kv, _, err = c.client.KV().Get(c.kvKey, qo)
	if nil != err {
		return nil, err
	}

	if nil == kv {
		return nil, fmt.Errorf("kv \"%s\" not found in datacenter \"%s\"", c.kvKey, dc)
	}

	if kv.Session != "" {
		se, _, err = c.client.Session().Info(kv.Session, qo)
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

// Wait will block until a leader has been elected, regardless of candidate.
func (c *Candidate) Wait() {
	var err error

	for {
		// attempt to locate current leader
		if _, err = c.LeaderService(); nil == err {
			break
		}

		c.log.Debugf("Error locating leader service: %s", err)

		// if session empty, assume no leader elected yet and try again
		time.Sleep(time.Second * 1)
	}
}

func (c *Candidate) sessionUpdate(update session.Update) {
	c.mu.Lock()
	if update.Error != nil {
		c.log.Printf("Session update errored, leaving election: %s", update.Error)
	}
	c.mu.Unlock()
}

// acquire will attempt to do just that.  Caller must hold lock!
func (c *Candidate) acquire(sid string) (bool, error) {
	var err error
	var elected bool

	kvpValue := struct {
		Candidate string
		Acquired  time.Time
	}{
		Candidate: c.id,
		Acquired:  time.Now(),
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

func (c *Candidate) lockRunner() {
	var sid string
	var elected bool
	var resigned chan struct{}
	var err error

	c.session.Run()

	interval := c.session.RenewInterval()

	acquireTicker := time.NewTicker(interval)

acquisition:
	for {
		select {
		case <-acquireTicker.C:
			c.mu.Lock()
			if sid = c.session.ID(); sid == "" {
				c.log.Debugf("Session does not exist, will try locking again in \"%d\" seconds...", int64(interval.Seconds()))
			} else if elected, err = c.acquire(sid); err != nil {
				c.log.Printf("Error attempting to acquire lock: %s", err)
			}
			if elected != c.elected {
				c.elected = elected
				c.watchers.notify(elected)
			}
			c.mu.Unlock()

		case resigned = <-c.resign:
			break acquisition
		}
	}

	c.mu.Lock()

	acquireTicker.Stop()

	c.log.Debug("Resigning...")

	c.elected = false
	c.session.Stop()
	c.watchers.notify(false)

	// for good measure..
	c.state = StateResigned

	if resigned != nil {
		resigned <- struct{}{}
	}

	c.log.Print("Resigned")

	c.mu.Unlock()
}
