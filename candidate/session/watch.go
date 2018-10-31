package session

import (
	"github.com/myENA/consultant/util"
	"sync"
)

type WatchFunc func(update Update)

type watchers struct {
	mu    sync.RWMutex
	funcs map[string]WatchFunc
}

func newWatchers() *watchers {
	w := &watchers{
		funcs: make(map[string]WatchFunc),
	}
	return w
}

// Watch allows you to register a function that will be called when the election State has changed
func (c *watchers) Add(id string, fn WatchFunc) string {
	c.mu.Lock()
	if id == "" {
		id = util.RandStr(8)
	}
	_, ok := c.funcs[id]
	if !ok {
		c.funcs[id] = fn
	}
	c.mu.Unlock()
	return id
}

// Unwatch will remove a function from the list of watchers.
func (c *watchers) Remove(id string) {
	c.mu.Lock()
	delete(c.funcs, id)
	c.mu.Unlock()
}

func (c *watchers) RemoveAll() {
	c.mu.Lock()
	c.funcs = make(map[string]WatchFunc)
	c.mu.Unlock()
}

// notifyWatchers is a thread safe update of leader status
func (c *watchers) notify(update Update) {
	c.mu.RLock()
	for _, fn := range c.funcs {
		go fn(update)
	}
	c.mu.RUnlock()
}
