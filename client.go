package consultant

import (
	"errors"
	"fmt"
	"github.com/hashicorp/consul/api"
	"github.com/myENA/consultant/log"
	"github.com/myENA/consultant/service"
	"github.com/myENA/consultant/util"
	"math/rand"
	"net/url"
	"sort"
	"sync"
)

type Client struct {
	*api.Client

	log log.DebugLogger

	config api.Config

	myAddress   string
	myAddressMu sync.RWMutex

	myHostname   string
	myHostnameMu sync.RWMutex

	myNode string

	logSlug      string
	logSlugSlice []interface{}
}

type TagsOption service.TagsOption

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

	if c.myNode, err = c.Agent().NodeName(); err != nil {
		return nil, fmt.Errorf("unable to determine local Consul node name: %s", err)
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

// MyAddr will either return the address returned by util.MyAddress() or the value set by client.SetMyAddr()
func (c *Client) MyAddr() string {
	var addr string
	c.myAddressMu.RLock()
	if c.myAddress == "" {
		addr, _ = util.MyAddress()
	} else {
		addr = c.myAddress
	}
	c.myAddressMu.RUnlock()
	return addr
}

// SetMyAddr allows you to manually specify the IP address of our host for this client
func (c *Client) SetMyAddr(myAddr string) {
	c.myAddressMu.Lock()
	c.myAddress = myAddr
	c.myAddressMu.Unlock()
}

// MyHost will either return the value returned by util.MyHostname() or the value set by client.SetMyHost()
func (c *Client) MyHost() string {
	var hostname string
	c.myHostnameMu.RLock()
	if c.myHostname == "" {
		hostname, _ = util.MyHostname()
	} else {
		hostname = c.myHostname
	}
	c.myHostnameMu.RUnlock()
	return hostname
}

// SetMyHost allows you to to manually specify the name of our host for this client
func (c *Client) SetMyHost(myHost string) {
	c.myHostnameMu.Lock()
	c.myHostname = myHost
	c.myHostnameMu.Unlock()
}

// MyNode returns the name of the Consul Node this client is connected to
func (c *Client) MyNode() string {
	return c.myNode
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

// PickService will attempt to locate any registered service with a name + tag combination and return one at random from
// the resulting list
func (c *Client) PickService(service, tag string, passingOnly bool, options *api.QueryOptions) (*api.ServiceEntry, *api.QueryMeta, error) {
	svcs, qm, err := c.Health().Service(service, tag, passingOnly, options)
	if err != nil {
		return nil, nil, err
	}

	svcLen := len(svcs)
	if 0 < svcLen {
		return svcs[rand.Intn(svcLen)], qm, nil
	}

	return nil, qm, nil
}

// ServiceByTags - this wraps the consul Health().Service() call, adding the tagsOption parameter and accepting a
// slice of tags.  tagsOption should be one of the following:
//
//     TagsAll - this will return only services that have all the specified tags present.
//     TagsExactly - like TagsAll, but will return only services that match exactly the tags specified, no more.
//     TagsAny - this will return services that match any of the tags specified.
//     TagsExclude - this will return services don't have any of the tags specified.
func (c *Client) ServiceByTags(service string, tags []string, tagsOption TagsOption, passingOnly bool, options *api.QueryOptions) ([]*api.ServiceEntry, *api.QueryMeta, error) {

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
// DEPRECATED in favor of service.SimpleRegistration
type SimpleServiceRegistration struct {
	Name string // [required] name to register service under
	Port int    // [required] external port to advertise for service consumers

	ID                string   // [optional] specific id for service, will be generated if not set
	RandomID          bool     // [optional] if ID is not set, use a random uuid if true, or hostname if false
	Address           string   // [optional] determined automatically by RegisterSimple() if not set
	Tags              []string // [optional] desired tags: RegisterSimple() adds serviceId
	CheckTCP          bool     // [optional] if true register a TCP check
	CheckPath         string   // [optional] register an http check with this path if set
	CheckScheme       string   // [optional] override the http check scheme (default: http)
	CheckPort         int      // [optional] if set, this is the port that the health check lives at
	Interval          string   // [optional] check interval
	EnableTagOverride bool     // [optional] whether we should allow tag overriding (new in 0.6+)
}

// SimpleServiceRegister is a helper method to ease consul service registration
// DEPRECATED in favor of service.RegisterSimple
func (c *Client) SimpleServiceRegister(reg *SimpleServiceRegistration) (string, error) {
	return service.RegisterSimple(
		&service.SimpleRegistration{
			Name:              reg.Name,
			Port:              reg.Port,
			ID:                reg.ID,
			RandomID:          reg.RandomID,
			Address:           reg.Address,
			EnableTagOverride: reg.EnableTagOverride,
			Tags:              reg.Tags,
			CheckPort:         reg.CheckPort,
			CheckInterval:     reg.Interval,
			CheckTCP:          reg.CheckTCP,
			CheckPath:         reg.CheckPath,
			CheckScheme:       reg.CheckScheme,
		},
		nil)
}
