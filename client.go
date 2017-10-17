package consultant

import (
	"errors"
	"fmt"
	"github.com/hashicorp/consul/api"
	"github.com/renstrom/shortuuid"
	"math/rand"
	"net/url"
	"os"
	"strings"
)

type Client struct {
	*api.Client

	config api.Config

	myAddr string
	myHost string

	myNode string

	logSlug      string
	logSlugSlice []interface{}
}

// NewClient constructs a new consultant client.
func NewClient(conf *api.Config) (*Client, error) {
	var err error

	if nil == conf {
		return nil, errors.New("config cannot be nil")
	}

	client := &Client{
		config:       *conf,
		logSlug:      "[consultant-client]",
		logSlugSlice: []interface{}{"[consultant-client]"},
	}

	client.Client, err = api.NewClient(conf)
	if err != nil {
		return nil, fmt.Errorf("Unable to create Consul API Client: %s", err)
	}

	if client.myHost, err = os.Hostname(); err != nil {
		client.logPrintf("Unable to determine hostname: %s", err)
	}

	if client.myAddr, err = GetMyAddress(); err != nil {
		client.logPrintf("Unable to determine ip address: %s", err)
	}

	if client.myNode, err = client.Agent().NodeName(); err != nil {
		return nil, fmt.Errorf("Unable to determine local Consul node name: %s", err)
	}

	return client, nil
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
	if nil != err {
		return nil, nil, err
	}

	svcLen := len(svcs)
	if 0 < svcLen {
		return svcs[rand.Intn(svcLen)], qm, nil
	}

	return nil, qm, nil
}

// BuildServiceURL will attempt to locate a healthy instance of the specified service name + tag combination, then
// attempt to construct a *net.URL from the resulting service information
func (c *Client) BuildServiceURL(protocol, serviceName, tag string, passingOnly bool, options *api.QueryOptions) (*url.URL, error) {
	svc, _, err := c.PickService(serviceName, tag, passingOnly, options)
	if nil != err {
		return nil, err
	}
	if nil == svc {
		return nil, fmt.Errorf("No services registered as \"%s\" with tag \"%s\" found.", serviceName, tag)
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
	var checkTCP *api.AgentServiceCheck  // tcp port check
	var serviceName string               // service registration name

	// Perform some basic service name cleanup and validation
	serviceName = strings.TrimSpace(reg.Name)
	if serviceName == "" {
		return "", errors.New("\"Name\" cannot be blank")
	}
	if strings.Contains(serviceName, " ") {
		return "", fmt.Errorf("Specified service name \"%s\" is invalid, service names cannot contain spaces", serviceName)
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
			tail = shortuuid.New()
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
			Interval: reg.Interval,
		}

		// add http check
		asr.Checks = append(asr.Checks, checkHTTP)
	}

	// build tcp check if specified
	if reg.CheckTCP {
		// create tcp check definition
		checkTCP = &api.AgentServiceCheck{
			TCP:      fmt.Sprintf("%s:%d", address, checkPort),
			Interval: interval,
		}
	}

	// add tcp check if defined
	if checkTCP != nil {
		asr.Checks = append(asr.Checks, checkTCP)
	}

	// register and check error
	if err = c.Agent().ServiceRegister(asr); err != nil {
		return "", err
	}

	// return registered service
	return serviceID, nil
}

func (c *Client) logPrintf(format string, v ...interface{}) {
	log.Printf(fmt.Sprintf("%s %s", c.logSlug, format), v...)
}

func (c *Client) logPrint(v ...interface{}) {
	log.Print(append(c.logSlugSlice, v...)...)
}

func (c *Client) logPrintln(v ...interface{}) {
	log.Println(append(c.logSlugSlice, v...)...)
}

func (c *Client) logFatalf(format string, v ...interface{}) {
	log.Fatalf(fmt.Sprintf("%s %s", c.logSlug, format), v...)
}

func (c *Client) logFatal(v ...interface{}) {
	log.Fatal(append(c.logSlugSlice, v...)...)
}

func (c *Client) logFatalln(v ...interface{}) {
	log.Fatalln(append(c.logSlugSlice, v...)...)
}

func (c *Client) logPanicf(format string, v ...interface{}) {
	log.Panicf(fmt.Sprintf("%s %s", c.logSlug, format), v...)
}

func (c *Client) logPanic(v ...interface{}) {
	log.Panic(append(c.logSlugSlice, v...)...)
}

func (c *Client) logPanicln(v ...interface{}) {
	log.Panicln(append(c.logSlugSlice, v...)...)
}
