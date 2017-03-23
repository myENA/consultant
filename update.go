package consultant

import (
	"fmt"
	"math/rand"
)

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

func randToken() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}
