package consultant

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/hashicorp/consul/api"
	"github.com/myENA/consultant/log"
	"github.com/myENA/consultant/util"
)

type Client struct {
	*api.Client

	log log.DebugLogger

	config api.Config

	myAddr string
	myHost string

	myNodeMu sync.Mutex
	myNode   string

	logSlug      string
	logSlugSlice []interface{}
}

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

// NewClient constructs a new consultant client.
func NewClient(conf *api.Config) (*Client, error) {
	var err error

	if nil == conf {
		return nil, errors.New("config cannot be nil")
	}

	c := &Client{
		config: *conf,
		log:    log.New("consultant-client"),
	}

	c.Client, err = api.NewClient(conf)
	if err != nil {
		return nil, fmt.Errorf("unable to create Consul API Client: %s", err)
	}

	if c.myHost, err = os.Hostname(); err != nil {
		c.log.Printf("Unable to determine hostname: %s", err)
	}

	if c.myAddr, err = util.MyAddress(); err != nil {
		c.log.Printf("Unable to determine ip address: %s", err)
	}

	return c, nil
}

// NewDefaultClient creates a new client with default configuration values
func NewDefaultClient() (*Client, error) {
	return NewClient(api.DefaultConfig())
}

// Config returns the API Client configuration struct as it was at time of construction
func (c *Client) Config() api.Config {
	return c.config
}

// MyAddr returns either the self-determine or set IP address of our host
func (c *Client) MyAddr() string {
	return c.myAddr
}

// SetMyAddr allows you to manually specify the IP address of our host
func (c *Client) SetMyAddr(myAddr string) {
	c.myAddr = myAddr
}

// MyHost returns either the self-determined or set name of our host
func (c *Client) MyHost() string {
	return c.myHost
}

// SetMyHost allows you to to manually specify the name of our host
func (c *Client) SetMyHost(myHost string) {
	c.myHost = myHost
}

// MyNode returns the name of the Consul Node this client is connected to
func (c *Client) MyNode() string {
	c.myNodeMu.Lock()
	if c.myNode == "" {
		var err error
		if c.myNode, err = c.Agent().NodeName(); err != nil {
			c.log.Printf("unable to determine local Consul node name: %s", err)
		}
	}
	n := c.myNode
	c.myNodeMu.Unlock()
	return n
}

// EnsureKey will fetch a key/value and ensure the key is present.  The value may still be empty.
func (c *Client) EnsureKey(key string, options *api.QueryOptions) (*api.KVPair, *api.QueryMeta, error) {
	kvp, qm, err := c.KV().Get(key, options)
	if err != nil {
		return nil, nil, err
	}
	if kvp != nil {
		return kvp, qm, nil
	}
	return nil, nil, errors.New("key not found")
}

// EnsureKeyString is a convenience method that will typecast the kvp.Value byte slice to a string for you, if there were no errors.
func (c *Client) EnsureKeyString(key string, options *api.QueryOptions) (string, *api.QueryMeta, error) {
	if kvp, qm, err := c.EnsureKey(key, options); err != nil {
		return "", qm, err
	} else {
		return string(kvp.Value), qm, nil
	}
}

// EnsureKeyJSON will attempt to unmarshal the kvp value into the provided type
func (c *Client) EnsureKeyJSON(key string, options *api.QueryOptions, v interface{}) (*api.QueryMeta, error) {
	if kvp, qm, err := c.EnsureKey(key, options); err != nil {
		return nil, err
	} else if err = json.Unmarshal(kvp.Value, v); err != nil {
		return nil, err
	} else {
		return qm, nil
	}
}

// PickServiceMultipleTags will attempt to locate any registered service with a name + tags combination and return one
// at random from the resulting list
func (c *Client) PickServiceMultipleTags(service string, tags []string, passingOnly bool, options *api.QueryOptions) (*api.ServiceEntry, *api.QueryMeta, error) {
	svcs, qm, err := c.Health().ServiceMultipleTags(service, tags, passingOnly, options)
	if err != nil {
		return nil, nil, err
	}

	svcLen := len(svcs)
	switch svcLen {
	case 0:
		return nil, qm, nil
	case 1:
		return svcs[0], qm, nil
	default:
		return svcs[rand.Intn(svcLen)], qm, nil
	}
}

// PickService will attempt to locate any registered service with a name + tag combination and return one at random from
// the resulting list
func (c *Client) PickService(service, tag string, passingOnly bool, options *api.QueryOptions) (*api.ServiceEntry, *api.QueryMeta, error) {
	if tag == "" {
		return c.PickServiceMultipleTags(service, nil, passingOnly, options)
	}
	return c.PickServiceMultipleTags(service, []string{tag}, passingOnly, options)
}

// EnsureServiceMultipleTags will return an error for an actual client error or if no service was found using the
// provided criteria
func (c *Client) EnsureServiceMultipleTags(service string, tags []string, passingOnly bool, options *api.QueryOptions) (*api.ServiceEntry, *api.QueryMeta, error) {
	if svc, qm, err := c.PickServiceMultipleTags(service, tags, passingOnly, options); err != nil {
		return nil, qm, err
	} else if svc == nil {
		return nil, qm, fmt.Errorf("service %q with tag %q not found", service, tags)
	} else {
		return svc, qm, nil
	}
}

// EnsureService will return an error for an actual client error or if no service was found using the provided criteria
func (c *Client) EnsureService(service, tag string, passingOnly bool, options *api.QueryOptions) (*api.ServiceEntry, *api.QueryMeta, error) {
	return c.EnsureServiceMultipleTags(service, []string{tag}, passingOnly, options)
}

// ServiceByTags - this wraps the consul Health().Service() call, adding the tagsOption parameter and accepting a
// slice of tags.  tagsOption should be one of the following:
//
//     TagsAll - this will return only services that have all the specified tags present.
//     TagsExactly - like TagsAll, but will return only services that match exactly the tags specified, no more.
//     TagsAny - this will return services that match any of the tags specified.
//     TagsExclude - this will return services don't have any of the tags specified.
func (c *Client) ServiceByTags(service string, tags []string, tagsOption TagsOption, passingOnly bool, options *api.QueryOptions) ([]*api.ServiceEntry, *api.QueryMeta, error) {
	if tagsOption == TagsAll {
		return c.Client.Health().ServiceMultipleTags(service, tags, passingOnly, options)
	}

	var (
		retv, tmpv []*api.ServiceEntry
		qm         *api.QueryMeta
		err        error
	)

	// Grab the initial payload
	switch tagsOption {
	case TagsExactly:
		// initially limit response by only retrieving a list of services that have at least the specified tags
		tmpv, qm, err = c.Health().ServiceMultipleTags(service, tags, passingOnly, options)
	case TagsAny, TagsExclude:
		// do not limit response by tags initially
		tmpv, qm, err = c.Health().Service(service, "", passingOnly, options)
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

// BuildServiceURL will attempt to locate a healthy instance of the specified service name + tag combination, then
// attempt to construct a *net.URL from the resulting service information
func (c *Client) BuildServiceURL(protocol, serviceName, tag string, passingOnly bool, options *api.QueryOptions) (*url.URL, error) {
	svc, _, err := c.PickService(serviceName, tag, passingOnly, options)
	if err != nil {
		return nil, err
	}
	if nil == svc {
		return nil, fmt.Errorf("no services registered as \"%s\" with tag \"%s\" found", serviceName, tag)
	}

	return url.Parse(fmt.Sprintf("%s://%s:%d", protocol, svc.Service.Address, svc.Service.Port))
}

// SimpleServiceRegistration describes a service that we want to register
type SimpleServiceRegistration struct {
	Name string // [required] name to register service under
	Port int    // [required] external port to advertise for service consumers

	ID                string   // [optional] specific id for service, will be generated if not set
	RandomID          bool     // [optional] if ID is not set, use a random uuid if true, or hostname if false
	Address           string   // [optional] determined automatically by Register() if not set
	Tags              []string // [optional] desired tags: Register() adds serviceId
	CheckTCP          bool     // [optional] if true register a TCP check
	CheckPath         string   // [optional] register an http check with this path if set
	CheckScheme       string   // [optional] override the http check scheme (default: http)
	CheckPort         int      // [optional] if set, this is the port that the health check lives at
	Interval          string   // [optional] check interval
	EnableTagOverride bool     // [optional] whether we should allow tag overriding (new in 0.6+)
}

// SimpleServiceRegister is a helper method to ease consul service registration
func (c *Client) SimpleServiceRegister(reg *SimpleServiceRegistration) (string, error) {
	var err error                        // generic error holder
	var serviceID string                 // local service identifier
	var address string                   // service host address
	var interval string                  // check interval
	var checkHTTP *api.AgentServiceCheck // http type check
	var serviceName string               // service registration name

	// Perform some basic service name cleanup and validation
	serviceName = strings.TrimSpace(reg.Name)
	if serviceName == "" {
		return "", errors.New("\"Name\" cannot be blank")
	}
	if strings.Contains(serviceName, " ") {
		return "", fmt.Errorf("name \"%s\" is invalid, service names cannot contain spaces", serviceName)
	}

	// Come on, guys...valid ports plz...
	if reg.Port <= 0 {
		return "", fmt.Errorf("%d is not a valid port", reg.Port)
	}

	if address = reg.Address; address == "" {
		address = c.myAddr
	}

	if serviceID = reg.ID; serviceID == "" {
		// Form a unique service id
		var tail string
		if reg.RandomID {
			tail = util.RandStr(12)
		} else {
			tail = strings.ToLower(c.myHost)
		}
		serviceID = fmt.Sprintf("%s-%s", serviceName, tail)
	}

	if interval = reg.Interval; interval == "" {
		// set a default interval
		interval = "30s"
	}

	// The serviceID is added in order to ensure detection in ServiceMonitor()
	tags := append(reg.Tags, serviceID)

	// Set up the service registration struct
	asr := &api.AgentServiceRegistration{
		ID:                serviceID,
		Name:              serviceName,
		Tags:              tags,
		Port:              reg.Port,
		Address:           address,
		Checks:            api.AgentServiceChecks{},
		EnableTagOverride: reg.EnableTagOverride,
	}

	// allow port override
	checkPort := reg.CheckPort
	if checkPort <= 0 {
		checkPort = reg.Port
	}

	// build http check if specified
	if reg.CheckPath != "" {

		// allow scheme override
		checkScheme := reg.CheckScheme
		if checkScheme == "" {
			checkScheme = "http"
		}

		// build check url
		checkURL := &url.URL{
			Scheme: checkScheme,
			Host:   fmt.Sprintf("%s:%d", address, checkPort),
			Path:   reg.CheckPath,
		}

		// build check
		checkHTTP = &api.AgentServiceCheck{
			HTTP:     checkURL.String(),
			Interval: interval,
		}

		// add http check
		asr.Checks = append(asr.Checks, checkHTTP)
	}

	// build tcp check if specified
	if reg.CheckTCP {
		// create tcp check definition
		asr.Checks = append(asr.Checks, &api.AgentServiceCheck{
			TCP:      fmt.Sprintf("%s:%d", address, checkPort),
			Interval: interval,
		})
	}

	// register and check error
	if err = c.Agent().ServiceRegister(asr); err != nil {
		return "", err
	}

	// return registered service
	return serviceID, nil
}
