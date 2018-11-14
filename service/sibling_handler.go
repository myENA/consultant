package service

import (
	"log"
	"sync"

	"github.com/hashicorp/consul/watch"
)

// TODO: refactor into semi-generic filtering and broadcast system.

type (
	// SiblingLocatorConfig
	//
	// This config defines the exclusion rules used to filter out a the local service from the list of similar services
	//
	// For example, if you had 3 of the same service registering to the same consul agent node, you would omit the
	// service considered "local" to the owner of this handler by passing in the ID of your local service.
	//
	// If you spread out this service such that it registers against multiple individual consul agent nodes, you would
	// pass in the name of the local node.
	//
	// You can, of course, combine both if you have a mix of same and cross node service registrations.
	SiblingLocatorConfig struct {

		// Service Watch configuration

		// [REQUIRED] - The name of the service you wish to watch
		ServiceName string
		// [OPTIONAL] - Any service tags you wish to restrict the watch plan to
		ServiceTags []string

		// Update Filtering configuration

		// [OPTIONAL] - The ID you wish to exclude from updates
		FilterServiceIDs []string
		// [OPTIONAL] - The Node you wish to exclude from updates
		FilterNodes []string
		// [OPTIONAL] -
		FilterTags []string

		// Watch Client configuration

		// [OPTIONAL] - Address of consul agent to use
		Address string
		// [OPTIONAL] - Logger to pass into watch plan
		Logger *log.Logger
	}

	// SiblingLocator is a pre-built hybrid handler that is designed to help broadcast sibling service changes
	// to multiple receivers
	SiblingLocator struct {
		mu       sync.RWMutex
		config   *SiblingLocatorConfig
		wp       *watch.Plan
		watchers map[string]interface{}
	}
)

func NewSiblingLocator(config *SiblingLocatorConfig) (*SiblingLocator, error) {
	sl := SiblingLocator{
		config:   config,
		watchers: make(map[string]interface{}),
	}

	return &sl, nil
}
