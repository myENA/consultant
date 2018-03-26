package service

import (
	"fmt"
	"github.com/hashicorp/consul/api"
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

func ByTags(service string, tags []string, tagsOption TagsOption, passingOnly bool, options *api.QueryOptions, client *api.Client) ([]*api.ServiceEntry, *api.QueryMeta, error) {
	var (
		retv, tmpv []*api.ServiceEntry
		qm         *api.QueryMeta
		err        error
	)

	if client == nil {
		if defaultClient == nil {

		}
		client = defaultClient
	}

	tag := ""

	// Grab the initial payload
	switch tagsOption {
	case TagsAll, TagsExactly:
		if len(tags) > 0 {
			tag = tags[0]
		}
		fallthrough
	case TagsAny, TagsExclude:
		tmpv, qm, err = c.Health().Service(service, tag, passingOnly, options)
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
