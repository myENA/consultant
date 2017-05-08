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
	myaddr string
	myhost string
}

// SimpleServiceRegistration describes a service that we want to register
type SimpleServiceRegistration struct {
	Name              string
	Id                string   // assigned by Register() if not set
	RandomId          bool     // if Id is not set, use a random uuid if true, or hostname if false
	Address           string   // determined automatically by Register() if not set
	Tags              []string // desired tags: Register() adds serviceId
	Port              int      // external port to advertise for service consumers
	CheckTCP          bool     // if true register a TCP check
	CheckPath         string   // register an http check with this path if set
	CheckScheme       string   // optionally override the http check scheme (default: http)
	CheckPort         int      // if set, this is the port that the health check lives at
	Interval          string   // check interval
	EnableTagOverride bool     // whether we should allow tag overriding (new in 0.6+)
}

// NewCustomConnection generate a fresh connection to the local consul agent using a custom config
func NewCustomClient(c *api.Config) (*Client, error) {
	var err error
	var myhost, myaddr string

	if myhost, err = os.Hostname(); err != nil {
		return nil, fmt.Errorf("Unable to determine hostname: %s", err)
	}

	if myaddr, err = GetMyAddress(); err != nil {
		return nil, fmt.Errorf("Unable to determine my ip address: %s", err)
	}

	client, err := api.NewClient(c)
	if err != nil {
		return nil, err
	}

	return &Client{client, myaddr, myhost}, nil
}

// NewConnection generates a fresh connection to the local consul agent using the default config
func NewClient() (*Client, error) {
	return NewCustomClient(nil)
}

// Service will attempt to locate any registered service with a name + tag combination and return one at random from
// the resulting list
func (c *Client) Service(service, tag string, options *api.QueryOptions) (*api.ServiceEntry, *api.QueryMeta, error) {
	svcs, qm, err := c.Health().Service(service, tag, false, options)
	if nil != err {
		return nil, nil, err
	}

	if 0 < len(svcs) {
		return svcs[rand.Intn(len(svcs))], qm, nil
	}

	return nil, qm, nil
}

func (c *Client) PassingService(service, tag string, options *api.QueryOptions) (*api.ServiceEntry, *api.QueryMeta, error) {
	svcs, qm, err := c.Health().Service(service, tag, true, options)
	if nil != err {
		return nil, nil, err
	}

	if 0 < len(svcs) {
		return svcs[rand.Intn(len(svcs))], qm, nil
	}

	return nil, qm, nil
}

// SimpleServiceRegister is a helper method to ease consul service registration
func (c *Client) SimpleServiceRegister(reg *SimpleServiceRegistration) (string, error) {
	var err error                        // generic error holder
	var serviceId string                 // local service identifier
	var address string                   // host address for check
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
		address = c.myaddr
	}

	if serviceId = reg.Id; serviceId == "" {
		// Form a unique service id
		var tail string
		if reg.RandomId {
			tail = shortuuid.New()
		} else {
			tail = strings.ToLower(c.myhost)
		}
		serviceId = fmt.Sprintf("%s-%s", serviceName, tail)
	}

	if interval = reg.Interval; interval == "" {
		// set a default interval
		interval = "30s"
	}

	// The serviceId is added in order to ensure detection in ServiceMonitor()
	tags := append(reg.Tags, serviceId)

	// Set up the service registration struct
	asr := &api.AgentServiceRegistration{
		ID:                serviceId,
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
	return serviceId, nil
}
