package candidate

import (
	"github.com/myENA/consultant/util"
	"sync"
)

type WatchFunc func(elected bool)

type watchers struct {
	mu    sync.Mutex
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
func (c *watchers) notify(state bool) {
	c.mu.Lock()

	// TODO: is there a reason to make this blocking? Methods should be quick and elected status churn is prevented elsewhere.
	for _, watcher := range c.funcs {
		go func(w WatchFunc) {
			w(state)
		}(watcher)
	}

	c.mu.Unlock()
}
