package consultant

import (
	"fmt"
	"github.com/myENA/consultant/candidate"
)

type Candidate struct {
	*candidate.Candidate
}

func updateCompatibility(u chan<- bool) candidate.WatchFunc {
	if u == nil {
		u = make(chan bool, 1)
	}
	return func(e bool) { u <- e }
}

// NewCandidate creates a new Candidate
//
// DEPRECATED: Will be removed in a future release
//
// - "client" must be a valid api client
//
// - "candidateID" should be an implementation-relevant unique identifier for this candidate
//
// - "key" must be the full path to a KV, it will be created if it doesn't already exist
//
// - "ttl" is the duration to set on the kv session ttl, will default to 30s if not specified
func NewCandidate(client *Client, candidateID, key, ttl string) (*Candidate, error) {
	var err error
	if client == nil {
		if client, err = NewDefaultClient(); err != nil {
			return nil, fmt.Errorf("unable to initialize client: %s", err)
		}
	}
	c, err := candidate.New(client.Client, candidateID, key, ttl)
	if err != nil {
		return nil, err
	}

	c.Run()

	return &Candidate{Candidate: c}, nil
}

// Register returns a channel for updates in leader status -
// only one message per candidate instance will be sent
//
// DEPRECATED:  Will be removed in a future release
func (c *Candidate) RegisterUpdate(id string) (string, chan bool) {
	u := make(chan bool, 1)
	id = c.Watch(id, updateCompatibility(u))
	return id, u
}

// DeregisterUpdate will remove an update chan from this candidate
//
// DEPRECATED:  Will be removed in a future release
func (c *Candidate) DeregisterUpdate(id string) {
	c.Unwatch(id)
}

// DeregisterUpdates will empty out the map of update channels
//
// DEPRECATED:  Will be removed in a future release
func (c *Candidate) DeregisterUpdates() {
	c.RemoveWatchers()
}

// CandidateSessionParts will be removed in a future release.
type CandidateSessionParts struct {
	*candidate.SessionParts
}

// ParseCandidateSessionName is provided so you don't have to parse it yourself :)
//
// DEPRECATED: Will be removed in a future release
func ParseCandidateSessionName(name string) (*CandidateSessionParts, error) {
	sp, err := candidate.ParseSessionName(name)
	if err != nil {
		return nil, err
	}
	return &CandidateSessionParts{SessionParts: sp}, nil
}
