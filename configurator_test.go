package consultant_test

import (
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"
	"net/http"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/testutil"
	"github.com/hashicorp/consul/watch"
	"github.com/myENA/consultant"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

const (
	// Test prefix monitoring
	prefix = "test/"

	key1   = "key1"
	val1   = "value 1"
	val1b  = "value 1 after change"

	key2   = "key2"
	val2   = 2
	val2b  = 42

	// Service monitoring
	serviceName = "myservice"
	serviceTag1 = "tag1"
	serviceTag2 = "tag2"
	servicePort = 3000
	servicePath = "/check"

)

func ExampleConfigurator() {
	// minimal implementation
	c := &config{}
	consul,err := consultant.NewDefaultClient()
	if err != nil {
		panic("no consul")
	}

	// initialize config from consul and set up watchers
	wp, err := consul.KVPrefixConfigurator(c, prefix)
	if err != nil {
		panic("trouble configurating")
	}

	// ensure that reads from the config are thread safe
	cc := c.copy() // work with a copy of the configuration
	fmt.Printf("var1=%s", cc.var1)

	wp.Stop()
}

func TestConfigurator(t *testing.T) {
	suite.Run(t, &ConfiguratorTestSuite{})
}

// Implement the Configurator interface
type config struct {
	var1 string
	var2 int
	service []*api.ServiceEntry
	t    *testing.T
	sync.RWMutex
}

// Update() handles both the initial settings and updates when anything under the prefix changes
func (c *config) Update(_ uint64, data interface{}) {

	var err error

	// Code accessing "config" should grab a read lock or use sync/atomic to achieve thread safety
	c.Lock()
	defer c.Unlock()

	// We may consider using Update() for services and other updates
	switch data.(type) {

	case api.KVPairs:
		kvps := data.(api.KVPairs)
		c.t.Logf("Update received %d KV pairs", len(kvps))
		for _, kvp := range kvps {
			c.t.Logf("key=%s, val=%s", kvp.Key, kvp.Value)
			switch kvp.Key {
			case prefix + key1:
				c.var1 = string(kvp.Value)
				c.t.Logf("c.var1=%s", c.var1)
			case prefix + key2:
				c.var2, err = strconv.Atoi(string(kvp.Value))
				if err != nil {
					c.t.Logf("key %s is not an int", key2)
				}
			}
		}

	case []*api.ServiceEntry:
		c.service = data.([]*api.ServiceEntry)
		c.t.Logf("Update: I have %d services in my list", len(c.service))

	default:
		c.t.Log("Typecast failed")
		c.t.Fail()
	}
}

func (c *config) copy() *config {
	c.RLock()
	defer c.RUnlock()
	cc := *c
	return &cc
}

// 1. Test that the consul KV config is transferred correctly
// 2. Verify that an update is reflected in the config object
func (cs *ConfiguratorTestSuite) TestKVInit() {
	var err error

	cs.buildKVTestData()

	config := &config{
		t: cs.T(),
	}

	var wp *watch.Plan
	wp, err = cs.client.KVPrefixConfigurator(config, prefix)

	require.Nil(cs.T(), err, "InitConfigurator(..., %s) failed: %s", prefix, err)

	// Check that config has what we expect
	require.Equal(cs.T(), val1, config.var1, "the initialized val1 is not what I expected")
	require.Equal(cs.T(), val2, config.var2, "the initialized val2 is not what I expected")

	kv1 := &api.KVPair{Key: prefix + key1, Value: []byte(val1b)}
	_, err = cs.client.KV().Put(kv1, nil)
	require.Nil(cs.T(), err, "Trouble changing the value of %s", key1)
	time.Sleep(time.Second)
	require.Equal(cs.T(), val1b, config.var1, "var1 is not what i expected after updating in consul")

	kv2 := &api.KVPair{Key: prefix + key2, Value: []byte(fmt.Sprintf("%d", val2b))}
	_, err = cs.client.KV().Put(kv2, nil)
	require.Nil(cs.T(), err, "Trouble changing the value of %s", key2)
	time.Sleep(time.Second)
	require.Equal(cs.T(), val2b, config.var2, "var2 is not what i expected after updating in consul")

	time.Sleep(time.Second)

	// report what is actually in the kv prefix now:
	kvps, _, err := cs.client.KV().List(prefix, nil)
	config.Update(0, kvps)
	cs.T().Logf("config after manual update: %+v", config)

	wp.Stop()
}

func (cs *ConfiguratorTestSuite) TestServiceInit() {

	// Fire up a simple health check
	portString := fmt.Sprintf(":%d",servicePort)
	http.HandleFunc(servicePath, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "Hello")
	})
	go http.ListenAndServe(portString, nil)

	// Register a service
	sr := &consultant.SimpleServiceRegistration{
		Name: serviceName,
		Port: servicePort,
		Address: "localhost",
		RandomID: true,
		Tags: []string{serviceTag1,serviceTag2},
		CheckPath: servicePath,
		Interval: "5s",
	}
	serviceID, err := cs.client.SimpleServiceRegister(sr)
	require.Nil(cs.T(), err, "Trouble registering the test service")
	cs.T().Logf("serviceID=%s",serviceID)

	config := &config{
		t: cs.T(),
	}

	wp, err := cs.client.ConfigureService(config, serviceName, serviceTag1, false,nil)
	require.Nil(cs.T(), err, "Unable to configure the service")

	cc := config.copy()
	require.Equal(cs.T(), 1, len(cc.service), "Expecting exactly one service here")

	se := cc.service[0]
	for _,check := range se.Checks {
		cs.T().Logf("check[%s]=%s",check.Name,check.Status)
	}

	time.Sleep(10*time.Second)

	// should be passing now
	cc = config.copy()
	require.Equal(cs.T(), 1, len(cc.service), "Expecting exactly one service here")

	se = cc.service[0]
	for _,check := range se.Checks {
		cs.T().Logf("check[%s]=%s",check.Name,check.Status)
	}

	wp.Stop()
}

func (cs *ConfiguratorTestSuite) buildKVTestData() {
	var err error

	kv1 := &api.KVPair{Key: prefix + key1, Value: []byte(val1)}
	_, err = cs.client.KV().Put(kv1, nil)
	require.Nil(cs.T(), err, "Failed storing key1/val1: %s", err)

	kv2 := &api.KVPair{Key: prefix + key2, Value: []byte(fmt.Sprintf("%d", val2))}
	_, err = cs.client.KV().Put(kv2, nil)
	require.Nil(cs.T(), err, "Failed storing key2/val2: %s", err)
}

type ConfiguratorTestSuite struct {
	suite.Suite

	// these values are cyclical, and should be re-defined per test method
	server *testutil.TestServer
	client *consultant.Client
}

// SetupTest is called before each method is run.
func (cs *ConfiguratorTestSuite) SetupTest() {
	cs.server, cs.client = makeServerAndClient(cs.T(), nil)
}

// TearDownTest is called after each method has been run.
func (cs *ConfiguratorTestSuite) TearDownTest() {
	if nil != cs.client {
		cs.client = nil
	}
	if nil != cs.server {
		// TODO: Stop seems to return an error when the process is killed...
		cs.server.Stop()
		cs.server = nil
	}
}

func (cs *ConfiguratorTestSuite) TearDownSuite() {
	cs.TearDownTest()
}
