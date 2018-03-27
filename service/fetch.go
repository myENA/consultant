package service

import (
	"errors"
	"fmt"
	"github.com/hashicorp/consul/api"
	"math/rand"
	"sort"
)

type TagsOption int

const (
	// TagsAll means all tags passed must be present, though other tags are okay
	TagsAll TagsOption = iota

	// TagsAny means any service having at least one of the tags are returned
	TagsAny

	// TagsExactly means that the service tags must match those passed exactly
	TagsExactly

	// TagsExclude means skip services that match any tags passed
	TagsExclude
)

func ByTags(name string, tags []string, tagsOption TagsOption, passingOnly bool, options *api.QueryOptions, client *api.Client) ([]*api.ServiceEntry, *api.QueryMeta, error) {
	var (
		retv, tmpv []*api.ServiceEntry
		qm         *api.QueryMeta
		err        error
	)

	client = apiClient(client)

	tag := ""

	// Grab the initial payload
	switch tagsOption {
	case TagsAll, TagsExactly:
		if len(tags) > 0 {
			tag = tags[0]
		}
		fallthrough
	case TagsAny, TagsExclude:
		tmpv, qm, err = client.Health().Service(name, tag, passingOnly, options)
	default:
		return nil, nil, fmt.Errorf("invalid value for tagsOption: %d", tagsOption)
	}

	if err != nil {
		return retv, qm, err
	}

	var tagsMap map[string]bool
	if tagsOption != TagsExactly {
		tagsMap = make(map[string]bool)
		for _, t := range tags {
			tagsMap[t] = true
		}
	}

	// Filter payload.
OUTER:
	for _, se := range tmpv {
		switch tagsOption {
		case TagsExactly:
			if !strSlicesEqual(tags, se.Service.Tags) {
				continue OUTER
			}
		case TagsAll:
			if len(se.Service.Tags) < len(tags) {
				continue OUTER
			}
			// Consul allows duplicate tags, so this workaround.  Boo.
			mc := 0
			dedupeMap := make(map[string]bool)
			for _, t := range se.Service.Tags {
				if tagsMap[t] && !dedupeMap[t] {
					mc++
					dedupeMap[t] = true
				}
			}
			if mc != len(tagsMap) {
				continue OUTER
			}
		case TagsAny, TagsExclude:
			found := false
			for _, t := range se.Service.Tags {
				if tagsMap[t] {
					found = true
					break
				}
			}
			if tagsOption == TagsAny {
				if !found && len(tags) > 0 {
					continue OUTER
				}
			} else if found { // TagsExclude
				continue OUTER
			}

		}
		retv = append(retv, se)
	}

	return retv, qm, err
}

// determines if a and b contain the same elements (order doesn't matter)
func strSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	ac := make([]string, len(a))
	bc := make([]string, len(b))
	copy(ac, a)
	copy(bc, b)
	sort.Strings(ac)
	sort.Strings(bc)
	for i, v := range ac {
		if bc[i] != v {
			return false
		}
	}
	return true
}

// Pick will attempt to pick one service at random from a list of services based on the provided criteria.  If no
// services are returned, you will see a nil entry and a nil error
func Pick(name, tag string, passingOnly bool, options *api.QueryOptions, client *api.Client) (*api.ServiceEntry, *api.QueryMeta, error) {
	client = apiClient(client)
	svcs, qm, err := client.Health().Service(name, tag, passingOnly, options)
	if err != nil {
		return nil, nil, err
	}

	svcLen := len(svcs)
	if 0 < svcLen {
		return svcs[rand.Intn(svcLen)], qm, nil
	}

	return nil, qm, nil
}

// Ensure will attempt to Pick a service based on the provided criteria, however if no service is found it will return
// an error.
func Ensure(name, tag string, passingOnly bool, options *api.QueryOptions, client *api.Client) (*api.ServiceEntry, *api.QueryMeta, error) {
	if svc, qm, err := Pick(name, tag, passingOnly, options, client); err == nil && svc == nil {
		return nil, qm, errors.New("no service found")
	} else {
		return svc, qm, err
	}
}
