package consultant

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"sync"
	"time"

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

func (t TagsOption) String() string {
	switch t {
	case TagsAll:
		return "all"
	case TagsAny:
		return "any"
	case TagsExactly:
		return "exactly"
	case TagsExclude:
		return "exclude"

	default:
		return "UNKNOWN"
	}
}

// Client is a thin wrapper around the base Consul API client with some convenience methods added
type Client struct {
	*api.Client

	localAddr     string
	localHostname string

	localNodeNameMu sync.Mutex
	localNodeName   string
	localNodeAddr   string

	confToken      string
	confDatacenter string
}

// NewClient constructs a new consultant client.
func NewClient(conf *api.Config) (*Client, error) {
	var (
		err error

		nodeAddrScheme   = "http"
		nodeAddrHostPort = "127.0.0.1:8500"
		c                = new(Client)
	)

	if nil == conf {
		return nil, errors.New("conf is required")
	}

	c.localHostname, _ = os.Hostname()
	c.localAddr, _ = LocalAddress()
	c.confToken = conf.Token
	c.confDatacenter = conf.Datacenter

	if c.Client, err = api.NewClient(conf); err != nil {
		return nil, fmt.Errorf("unable to create Consul API Client: %s", err)
	}

	if v := os.Getenv(api.HTTPSSLEnvName); v != "" {
		nodeAddrScheme = "https"
	}
	if v := os.Getenv(api.HTTPAddrEnvName); v != "" {
		nodeAddrHostPort = v
	}

	c.localNodeAddr = fmt.Sprintf("%s://%s", nodeAddrScheme, nodeAddrHostPort)

	return c, nil
}

// NewDefaultClient creates a new client with default configuration values
func NewDefaultClient() (*Client, error) {
	return NewClient(api.DefaultConfig())
}

// NewDefaultNonPooledClient creates a new client with default configuration values and a non-pooled http client
func NewDefaultNonPooledClient() (*Client, error) {
	return NewClient(api.DefaultNonPooledConfig())
}

// LocalAddress returns either the self-determine or set IP address of our host
func (c *Client) LocalAddress() string {
	return c.localAddr
}

// SetLocalAddress allows you to manually specify the IP address of our host
func (c *Client) SetLocalAddress(myAddr string) {
	c.localAddr = myAddr
}

// LocalHostname returns either the self-determined or set name of our host
func (c *Client) LocalHostname() string {
	return c.localHostname
}

// SetLocalHostname allows you to to manually specify the name of our host. This does NOT update the host's name.
func (c *Client) SetLocalHostname(myHost string) {
	c.localHostname = myHost
}

// LocalNodeName returns the name of the Consul Node this client is connected to
func (c *Client) LocalNodeName() (string, error) {
	c.localNodeNameMu.Lock()
	if c.localNodeName == "" {
		var err error
		if c.localNodeName, err = c.Agent().NodeName(); err != nil {
			return "", fmt.Errorf("unable to determine local Consul node name: %s", err)
		}
	}
	n := c.localNodeName
	c.localNodeNameMu.Unlock()
	return n, nil
}

// LocalNodeAddr returns the address of the Consul Node this client is connected to
func (c *Client) LocalNodeAddr() string {
	return c.localNodeAddr
}

// EnsureKey will fetch a key/value and ensure the key is present.  The value may still be empty.
func (c *Client) EnsureKey(key string, options *api.QueryOptions) (*api.KVPair, *api.QueryMeta, error) {
	var (
		kvp *api.KVPair
		qm  *api.QueryMeta
		err error
	)
	if kvp, qm, err = c.KV().Get(key, options); err != nil {
		return nil, nil, err
	}
	if kvp != nil {
		return kvp, qm, nil
	}
	return nil, nil, errors.New("key not found")
}

// EnsureKeyString is a convenience method that will typecast the kvp.Value byte slice to a string for you, if there were no errors.
func (c *Client) EnsureKeyString(key string, options *api.QueryOptions) (string, *api.QueryMeta, error) {
	var (
		kvp *api.KVPair
		qm  *api.QueryMeta
		err error
	)
	if kvp, qm, err = c.EnsureKey(key, options); err != nil {
		return "", qm, err
	}
	return string(kvp.Value), qm, nil
}

// EnsureKeyJSON will attempt to unmarshal the kvp value into the provided type
func (c *Client) EnsureKeyJSON(key string, options *api.QueryOptions, v interface{}) (*api.QueryMeta, error) {
	var (
		kvp *api.KVPair
		qm  *api.QueryMeta
		err error
	)
	if kvp, qm, err = c.EnsureKey(key, options); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(kvp.Value, v); err != nil {
		return nil, err
	}
	return qm, nil
}

// PickServiceMultipleTags will attempt to locate any registered service with a name + tags combination and return one
// at random from the resulting list
func (c *Client) PickServiceMultipleTags(service string, tags []string, passingOnly bool, options *api.QueryOptions) (*api.ServiceEntry, *api.QueryMeta, error) {
	svcs, qm, err := c.Health().ServiceMultipleTags(service, tags, passingOnly, options)
	if err != nil {
		return nil, nil, err
	}
	svcLen := len(svcs)
	var svc *api.ServiceEntry
	switch svcLen {
	case 0:
		return nil, qm, nil
	case 1:
		svc = svcs[0]
	default:
		svc = svcs[rand.Intn(svcLen)]
	}

	// Handle case where service address is blank, default to node address
	if svc.Service.Address == "" {
		svc.Service.Address = svc.Node.Address
	}

	return svc, qm, nil
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
	var (
		svc *api.ServiceEntry
		qm  *api.QueryMeta
		err error
	)
	if svc, qm, err = c.PickServiceMultipleTags(service, tags, passingOnly, options); err != nil {
		return nil, qm, err
	}
	if svc == nil {
		return nil, qm, fmt.Errorf("service %q with tag %q not found", service, tags)
	}
	return svc, qm, nil
}

// EnsureService will return an error for an actual client error or if no service was found using the provided criteria
func (c *Client) EnsureService(service, tag string, passingOnly bool, options *api.QueryOptions) (*api.ServiceEntry, *api.QueryMeta, error) {
	if tag == "" {
		return c.EnsureServiceMultipleTags(service, nil, passingOnly, options)
	}
	return c.EnsureServiceMultipleTags(service, []string{tag}, passingOnly, options)
}

// ServiceByTags - this wraps the consul Health().Service() call, adding the tagsOption parameter and accepting a
// slice of tags.  tagsOption should be one of the following:
//
//	TagsAll - this will return only services that have all the specified tags present.
//	TagsExactly - like TagsAll, but will return only services that match exactly the tags specified, no more.
//	TagsAny - this will return services that match any of the tags specified.
//	TagsExclude - this will return services don't have any of the tags specified.
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
		if se.Service.Address == "" {
			se.Service.Address = se.Node.Address
		}
		retv = append(retv, se)
	}

	return retv, qm, err
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

	ID                string        // [optional] specific id for service, will be generated if not set
	RandomID          bool          // [optional] if ID is not set, use a random uuid if true, or hostname if false
	Address           string        // [optional] determined automatically by Create() if not set
	Tags              []string      // [optional] desired tags: Create() adds serviceId
	CheckTCP          bool          // [optional] if true, register a TCP check
	CheckTTL          string        // [optional] if defined, registers a ttl check with the value as the starting status
	CheckPath         string        // [optional] if defined, register a http check with this path
	CheckScheme       string        // [optional] override the http check scheme (default: http)
	CheckPort         int           // [optional] tcp or http check port, if defined
	Interval          string        // [optional] check renewInterval
	Timeout           time.Duration // [optional] tcp
	EnableTagOverride bool          // [optional] whether we should allow tag overriding (new in 0.6+)
}

// SimpleServiceRegister is a helper method to ease consul service registration
func (c *Client) SimpleServiceRegister(reg *SimpleServiceRegistration) (string, error) {
	var (
		asr       *api.AgentServiceRegistration
		serviceID string
		nodeName  string
		err       error
	)

	nodeName, _ = c.LocalNodeName()

	if serviceID, asr, err = simpleToReg(c.LocalAddress(), nodeName, reg); err != nil {
		return serviceID, err
	}

	// register and check error
	if err = c.Agent().ServiceRegister(asr); err != nil {
		return "", err
	}

	// return registered service
	return serviceID, nil
}

// ManagedServiceRegister creates a new ManagedService instance from a SimpleServiceRegistration type
//
// You are not required to provide a *ManagedServiceConfig instance to this method, and you are
func (c *Client) ManagedServiceRegister(reg *SimpleServiceRegistration, cfg *ManagedServiceConfig, fns ...ManagedAgentServiceRegistrationMutator) (*ManagedService, error) {
	var (
		asr      *api.AgentServiceRegistration
		nodeName string
		err      error
	)

	nodeName, _ = c.LocalNodeName()

	if _, asr, err = simpleToReg(c.LocalAddress(), nodeName, reg); err != nil {
		return nil, err
	}

	msr := NewManagedAgentServiceRegistration(asr, fns...)

	if cfg == nil {
		cfg = new(ManagedServiceConfig)
	}

	cfg.Client = c.Client

	return msr.Create(cfg)
}
