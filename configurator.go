package consultant

import (
	"fmt"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/watch"
)

const (
	updateChanLength = 100 // must exceed the maximum number of watch plans
	chanLength       = 10  // a size for all other channels
)

// A config object that implements this interface can be initialized from consul's
// KV store or from it's service list and then automatically updated as these things change in consul.
type Configurator interface {
	UpdatePrefix(prefix string, index uint64, kvps api.KVPairs)
	UpdateService(name, tag string, passingOnly bool, index uint64, services []*api.ServiceEntry)
}

type serviceDetails struct {
	plan        *watch.Plan
	tag         string
	passingOnly bool
}

type update interface {
	Index() uint64
	Data() interface{}
}

type prefixUpdate struct {
	index  uint64
	data   interface{}
	prefix string
}

func (u *prefixUpdate) Index() uint64 {
	return u.index
}

func (u *prefixUpdate) Data() interface{} {
	return u.data
}

func (u *prefixUpdate) Prefix() string {
	return u.prefix
}

type serviceUpdate struct {
	index       uint64
	data        interface{}
	name        string
	tag         string
	passingOnly bool
}

func (u *serviceUpdate) Index() uint64 {
	return u.index
}

func (u *serviceUpdate) Data() interface{} {
	return u.data
}

func (u *serviceUpdate) Name() string {
	return u.name
}

func (u *serviceUpdate) Tag() string {
	return u.tag
}

func (u *serviceUpdate) PassingOnly() bool {
	return u.passingOnly
}

type ConfigChan chan Configurator

// ConfigManager keeps a private copy of the Configurator object in order to manage thread-safe access.
type ConfigManager struct {
	client        *Client                    // a consultant client
	config        Configurator               // private copy
	prefixPlans   map[string]*watch.Plan     // the prefixes we are managing
	servicePlans  map[string]*serviceDetails // services we are managing
	subscriptions map[ConfigChan]bool        // user can subscribe to updates
	seedChan      ConfigChan
	readChan      chan ConfigChan
	updateChan    chan update
	syncChan      chan chan chan bool
	stopChan      chan bool
}

// NewConfigManager creates a new instance and kicks off a manager for it
func (c *Client) NewConfigManager(config Configurator) *ConfigManager {

	cm := &ConfigManager{
		client:        c,
		config:        config,
		prefixPlans:   make(map[string]*watch.Plan),
		servicePlans:  make(map[string]*serviceDetails),
		subscriptions: make(map[ConfigChan]bool),
		seedChan:      make(ConfigChan, chanLength),
		readChan:      make(chan ConfigChan, chanLength),
		updateChan:    make(chan update, updateChanLength),
		syncChan:      make(chan chan chan bool),
		stopChan:      make(chan bool, 1),
	}

	// send in the config first so this is the first thing the handler sees
	cm.Seed(config)

	cm.configHandler()

	return cm
}

// Seed replaces the config handled by cm and updates the consul-dependent information
func (cm *ConfigManager) Seed(config Configurator) {
	cm.seedChan <- config
}

// Read retrieves the current configuration
func (cm *ConfigManager) Read() Configurator {
	req := make(ConfigChan)
	cm.readChan <- req
	return <-req
}

// Refresh all updates (in preparation for a read)
func (cm *ConfigManager) Refresh() *ConfigManager {
	sync := cm.pause()
	cm.updateAll()
	unpause(sync)
	return cm
}

func (cm *ConfigManager) configHandler() {
	go func() { //  make sure the handler is running before we return
	loop:
		for {
			select {

			// initialize the config object with non-consul items
			case seed := <-cm.seedChan:
				//log.Println("seedChan")
				cm.config = seed
				cm.updateAll()

			// request to get a copy of the config
			case req := <-cm.readChan:
				// Handle all updates before we serve the config back (push request back on channel)
				if len(cm.updateChan) > 0 {
					cm.readChan <- req
				} else {
					req <- cm.config
				}

			// updates are processed here
			case u := <-cm.updateChan:
				//log.Println("updateChan")
				switch u.(type) {
				case *prefixUpdate:
					up := u.(*prefixUpdate)
					d := up.Data()
					if d == nil {
						cm.config.UpdatePrefix(up.Prefix(), up.Index(), nil)
					} else {
						cm.config.UpdatePrefix(up.Prefix(), up.Index(), d.(api.KVPairs))
					}
				case *serviceUpdate:
					up := u.(*serviceUpdate)
					d := up.Data()
					if nil == d {
						cm.config.UpdateService(up.Name(), up.Tag(), up.PassingOnly(), up.Index(), nil)
					} else {
						cm.config.UpdateService(up.Name(), up.Tag(), up.PassingOnly(), u.Index(), d.([]*api.ServiceEntry))
					}

				default: // TODO: Yell about it?
					continue
				}

				// Serve subscribers once all updates have been processed
				if len(cm.updateChan) == 0 {
					cm.handleSubscriptions()
				}

			// provide a means to pause the handler
			case ch := <-cm.syncChan:
				//log.Println("syncChan")
				c1 := make(chan bool)
				ch <- c1 // say: do your work now
				<-c1     // wait until ready to move on again

			case <-cm.stopChan:
				//log.Println("stopChan")
				cm.cleanup()
				break loop
			}
		}
		log.Println("Exiting the handler")
	}()
}

// Trigger all updates
func (cm *ConfigManager) updateAll() {
	for prefix := range cm.prefixPlans {
		cm.updateKVPrefix(prefix)
	}
	for service, details := range cm.servicePlans {
		cm.updateService(service, details.tag, details.passingOnly)
	}
}

// transform a callback to a channel push
func (cm *ConfigManager) pushUpdate(u update) {
	cm.updateChan <- u
}

// Subscribe returns a channel that will send updates about the config
func (cm *ConfigManager) Subscribe() ConfigChan {
	ch := make(ConfigChan, 1)
	cm.subscriptions[ch] = true
	return ch
}

// Unsubscribe from channel updates by passing the channel here.
func (cm *ConfigManager) Unsubscribe(ch ConfigChan) {
	_, ok := cm.subscriptions[ch]
	if ok {
		delete(cm.subscriptions, ch)
	}
}

func (cm *ConfigManager) handleSubscriptions() {
	for ch := range cm.subscriptions {
		// Replace current item in the queue if there is something there
		if len(ch) == 1 {
			<-ch
		}
		ch <- cm.config
	}
}

// Stop shuts down the plans, channels, and the handler
func (cm *ConfigManager) Stop() {
	cm.stopChan <- true
}

// pause pauses the handler while we do some otherwise thread-unsafe stuff
func (cm *ConfigManager) pause() chan bool {
	c2 := make(chan chan bool)
	cm.syncChan <- c2
	return <-c2
}

// unpause tells the handler that it is okay to resume normal operations
func unpause(sync chan bool) {
	sync <- true
}

// AddKvPrefix starts watching the given prefix and updates the config with current values
func (cm *ConfigManager) AddKVPrefix(prefix string) error {

	var err error

	sync := cm.pause()
	defer unpause(sync)

	wp, ok := cm.prefixPlans[prefix]
	if ok {
		wp.Stop()
	}

	wp, err = cm.client.WatchKeyPrefix(prefix, true, func(index uint64, data interface{}) {
		cm.pushUpdate(&prefixUpdate{
			index:  index,
			data:   data,
			prefix: prefix,
		})
	})
	if err != nil {
		return fmt.Errorf("Trouble building the watch plan: %s", err)
	}

	go func() {
		err := wp.Run(cm.client.config.Address)
		if err != nil {
			log.Printf("Watch plan failed for prefix: %s", prefix)
		}
	}()

	cm.prefixPlans[prefix] = wp

	return nil
}

// AddService starts watching the specified service and updates the config
func (cm *ConfigManager) AddService(service, tag string, passingOnly bool) error {

	var err error

	sync := cm.pause()
	defer unpause(sync)

	details, ok := cm.servicePlans[service]
	if ok {
		details.plan.Stop()
	}

	details = &serviceDetails{
		tag:         tag,
		passingOnly: passingOnly,
	}

	details.plan, err = cm.client.WatchService(service, tag, passingOnly, true, func(index uint64, data interface{}) {
		cm.pushUpdate(&serviceUpdate{
			index:       index,
			data:        data,
			name:        service,
			tag:         tag,
			passingOnly: passingOnly,
		})
	})
	if err != nil {
		return fmt.Errorf("Trouble building the watch plan: %s", err)
	}

	go func() {
		err := details.plan.Run(cm.client.config.Address)
		if err != nil {
			fmt.Fprintf(details.plan.LogOutput, "Watch plan failed for service: %s", service)
		}
	}()

	cm.servicePlans[service] = details

	return nil
}

// updateKVPrefix - list kv:s in the prefix and update our config with the result
func (cm *ConfigManager) updateKVPrefix(prefix string) error {
	kvps, _, err := cm.client.KV().List(prefix, nil)
	if err != nil {
		return fmt.Errorf("Trouble getting the KVs under: %s", prefix)
	}
	cm.pushUpdate(&prefixUpdate{
		index:  0,
		data:   kvps,
		prefix: prefix,
	})

	return nil
}

// updateService lists current services and forces an update
func (cm *ConfigManager) updateService(service, tag string, passingOnly bool) error {
	seList, _, err := cm.client.Health().Service(service, tag, passingOnly, nil)
	if err != nil {
		return fmt.Errorf("Trouble finding a passing service for %s (tag=%s)", service, tag)
	}
	cm.pushUpdate(&serviceUpdate{
		index:       0,
		data:        seList,
		name:        service,
		tag:         tag,
		passingOnly: passingOnly,
	})

	return nil
}

// cleanup frees up resources in the ConfigManager
func (cm *ConfigManager) cleanup() {

	// Shut down the watch plans
	for _, details := range cm.servicePlans {
		details.plan.Stop()
	}

	for _, details := range cm.prefixPlans {
		details.Stop()
	}

	// Close subscriber channels
	for ch := range cm.subscriptions {
		close(ch)
	}
}
