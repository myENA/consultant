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

	logSlug      string
	logSlugSlice []interface{}
}

// NewConnection generates a fresh connection to the local consul agent using the default config
func NewClient(conf *api.Config) (*Client, error) {
	var err error

	client := &Client{
		logSlug:      "[consultant-client]",
		logSlugSlice: []interface{}{"[consultant-client]"},
	}

	client.Client, err = api.NewClient(conf)
	if err != nil {
		return nil, fmt.Errorf("Unable to create Consul API Client: %v", err)
	}

	if client.myhost, err = os.Hostname(); err != nil {
		client.logPrintf("Unable to determine hostname: %v", err)
	}

	if client.myaddr, err = GetMyAddress(); err != nil {
		client.logPrintf("Unable to determine ip address: %v", err)
	}

	return client, nil
}

// Service will attempt to locate any registered service with a name + tag combination and return one at random from
// the resulting list
func (c *Client) Service(service, tag string, passingOnly bool, options *api.QueryOptions) (*api.ServiceEntry, *api.QueryMeta, error) {
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

// SimpleServiceRegistration describes a service that we want to register
type SimpleServiceRegistration struct {
	Name string // [required] name to register service under
	Port int    // [required] external port to advertise for service consumers
	Id   string // [optional] specific id for service, will be generated if not set

	RandomId          bool     // [optional] if Id is not set, use a random uuid if true, or hostname if false
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
	var serviceId string                 // local service identifier
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
