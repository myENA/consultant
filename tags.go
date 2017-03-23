package consultant

import (
	"strings"
	"sync"

	"github.com/myENA/go-helpers"
)

type TagContainer struct {
	lock   *sync.RWMutex
	list   []string
	length int
}

func NewTagContainer(tags []string) *TagContainer {
	tmp := make([]string, len(tags))
	copy(tmp, tags)
	return &TagContainer{
		lock:   &sync.RWMutex{},
		list:   tmp,
		length: len(tmp),
	}
}

func (t *TagContainer) String() string {
	t.lock.RLock()
	defer t.lock.RUnlock()
	return strings.Join(t.list, ",")
}

func (t *TagContainer) List() []string {
	t.lock.RLock()
	defer t.lock.RUnlock()
	tmp := make([]string, t.length)
	copy(tmp, t.list)
	return tmp
}

func (t *TagContainer) Len() int {
	t.lock.RLock()
	defer t.lock.RUnlock()
	return t.length
}

func (t *TagContainer) Add(tags ...string) (delta int) {
	t.lock.Lock()
	defer t.lock.Unlock()

	var newList []string

	// if no input, return
	if 0 == len(tags) {
		return
	}

	// build new tag list and find difference count
	newList, delta = helpers.CombineStringSlices(t.list, tags)

	// if no difference, return
	if 0 == delta {
		return
	}

	// set new list
	t.list = newList

	// find new length
	t.length = len(t.list)

	return
}

func (t *TagContainer) Remove(tags ...string) (delta int) {
	t.lock.Lock()
	defer t.lock.Unlock()

	var newList []string

	// if no input, return
	if 0 == len(tags) {
		return
	}

	// build new list minus input
	newList, delta = helpers.RemoveStringsFromSlice(t.list, tags)

	// if no different, return
	if 0 == delta {
		return
	}

	// set new list
	t.list = newList

	// find new length
	t.length = len(t.list)

	return
}
