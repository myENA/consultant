package consultant

import (
	"fmt"

	"github.com/hashicorp/consul/watch"
)

const (
	updateChanLength = 100 // must exceed the maximum number of watch plans
	chanLength       = 10  //
)

// A config object that implements this interface can be initialized from consul's
// KV store or from it's service list and then automatically updated as these things change in consul.
type Configurator interface {
	Update(uint64, interface{})
}

type serviceDetails struct {
	plan        *watch.Plan
	tag         string
	passingOnly bool
}

type update struct {
	index uint64
	data  interface{}
}

type ConfigChan chan Configurator

// ConfigManager keeps a private copy of the Configurator object in order to manage thread-safe access.
type ConfigManager struct {
	client        *Client // a consultant client
	config        Configurator // private copy
	prefixPlans   map[string]*watch.Plan  // the prefixes we are managing
	servicePlans  map[string]*serviceDetails // services we are managing
	subscriptions map[*ConfigChan]bool // user can subscribe to updates
	seedChan      ConfigChan
	readChan      chan ConfigChan
	updateChan    chan update
	syncChan      chan chan chan bool
	stopChan      chan bool
}

// NewConfigManager creates a new instance and kicks off a manager for it
func (client *Client) NewConfigManager(config Configurator) *ConfigManager {

	cm := &ConfigManager{
		client:        client,
		config:        config,
		prefixPlans:   make(map[string]*watch.Plan),
		servicePlans:  make(map[string]*serviceDetails),
		subscriptions: make(map[*ConfigChan]bool),
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

func (cm *ConfigManager) configHandler() {
	go func() { //  make sure the handler is running before we return
	loop:
		for {
			select {

			// initialize the config object with non-consul items
			case seed := <-cm.seedChan:
				//fmt.Println("seedChan")
				cm.config = seed

				// fill in information from consul
				for prefix, _ := range cm.prefixPlans {
					cm.updateKVPrefix(prefix)
				}
				for service, details := range cm.servicePlans {
					cm.updateService(service, details.tag, details.passingOnly)
				}

			case req := <-cm.readChan:
				//fmt.Println("readChan")
				req <- cm.config

			case u := <-cm.updateChan:
				//fmt.Println("updateChan")
				cm.config.Update(u.index, u.data) // user-defined handling
				cm.handleSubscriptions()

			case ch := <-cm.syncChan:
				//fmt.Println("syncChan")
				c1 := make(chan bool)
				ch <- c1 // say: sync is yours
				<-c1     // wait until ready to move on again

			case <-cm.stopChan:
				//fmt.Println("stopChan")
				cm.cleanup()
				break loop
			}
		}
		fmt.Println("Exiting the handler")
	}()
}

// transform a callback to a channel push
func (cm *ConfigManager) updateHandler(index uint64, data interface{}) {
	cm.updateChan <- update{
		index: index,
		data:  data,
	}
}

func (cm *ConfigManager) Subscribe() *ConfigChan {
	ch := make(ConfigChan, 1)
	cm.subscriptions[&ch] = true
	return &ch
}

func (cm *ConfigManager) Unsubscribe(ch *ConfigChan) {
	_, ok := cm.subscriptions[ch]
	if ok {
		delete(cm.subscriptions, ch)
	}
}

func (cm *ConfigManager) handleSubscriptions() {
	for ch := range cm.subscriptions {
		// Replace current item in the queue if there is something there
		if len(*ch) == 1 {
			<-*ch
		}
		*ch <- cm.config
	}
}

// Stop shuts down the plans, channels, and the handler
func (cm *ConfigManager) Stop() {
	cm.stopChan <- true
}

// lock pauses the handler while we do some otherwise thread-unsafe stuff
func (cm *ConfigManager) lock() chan bool {
	c2 := make(chan chan bool)
	cm.syncChan <- c2
	return <-c2
}

// unlock tells the handler that it is okay to resume normal operations
func unlock(sync chan bool) {
	sync<-true
}

// AddKvPrefix starts watching the given prefix and updates the config with current values
func (cm *ConfigManager) AddKVPrefix(prefix string) error {

	var err error

	sync := cm.lock()
	defer unlock(sync)

	wp, ok := cm.prefixPlans[prefix]
	if ok {
		wp.Stop()
	}

	wp, err = cm.client.WatchKeyPrefix(prefix, true, cm.updateHandler)
	if err != nil {
		return fmt.Errorf("Trouble building the watch plan: %s", err)
	}

	go func() {
		err := wp.Run(cm.client.config.Address)
		if err != nil {
			fmt.Fprintf(wp.LogOutput, "Watch plan failed for prefix: %s", prefix)
		}
	}()

	cm.prefixPlans[prefix] = wp

	return nil
}

// AddService starts watching the specified service and updates the config
func (cm *ConfigManager) AddService(service, tag string, passingOnly bool) error {

	var err error

	sync := cm.lock()
	defer unlock(sync)

	details, ok := cm.servicePlans[service]
	if ok {
		details.plan.Stop()
	}

	details = &serviceDetails{
		tag:         tag,
		passingOnly: passingOnly,
	}

	details.plan, err = cm.client.WatchService(service, tag, passingOnly, true, cm.updateHandler)
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
	cm.updateHandler(0, kvps)

	return nil
}

// updateService lists current services and forces an update
func (cm *ConfigManager) updateService(service, tag string, passingOnly bool) error {
	seList, _, err := cm.client.Health().Service(service, tag, passingOnly, nil)
	if err != nil {
		return fmt.Errorf("Trouble finding a passing service for %s (tag=%s)", service, tag)
	}
	cm.updateHandler(0, seList)

	return nil
}

// cleanup frees up resources in the ConfigManager
func (cm *ConfigManager) cleanup() {

	// Shut down the watch plans
	for _, details := range cm.servicePlans {
		details.plan.Stop()
	}

	// Close subscriber channels
	for ch, _ := range cm.subscriptions {
		close(*ch)
	}
}
