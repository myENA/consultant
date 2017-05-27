package consultant_test

// Test this aspect of consultant with `go test -run Configurator -v`

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/testutil"
	"github.com/myENA/consultant"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

const (
	// Test prefix monitoring
	configuratorPrefix = "test/"

	configuratorKey1  = "key1"
	configuratorVal1  = "value 1"
	configuratorVal1b = "value 1 after change"

	configuratorKey2  = "key2"
	configuratorVal2  = 2
	configuratorVal2b = 42

	// Service monitoring
	configuratorServiceName = "myservice"
	configuratorServiceTag1 = "tag1"
	configuratorServiceTag2 = "tag2"
	configuratorServicePort = 3000
	configuratorServicePath = "/check"
)

func TestConfigurator(t *testing.T) {
	suite.Run(t, &ConfiguratorTestSuite{})
}

// Implement the Configurator interface
type config struct {
	var1    string
	var2    int
	otherVar int
	service []*api.ServiceEntry
	t       *testing.T
}

// Update() handles both the initial settings and updates when anything under the prefix changes
// or when there are changes to any registered services. Changes are made to a private copy of 'c'
// so thread safety is not an issue.
func (c *config) Update(_ uint64, data interface{}) {

	var err error

	switch data.(type) {

	case api.KVPairs:
		kvps := data.(api.KVPairs)
		c.t.Logf("Update received %d KV pairs", len(kvps))
		for _, kvp := range kvps {
			c.t.Logf("key=%s, val=%s", kvp.Key, kvp.Value)
			switch kvp.Key {
			case configuratorPrefix + configuratorKey1:
				c.var1 = string(kvp.Value)
			case configuratorPrefix + configuratorKey2:
				c.var2, err = strconv.Atoi(string(kvp.Value))
				if err != nil {
					c.t.Logf("key %s is not an int", configuratorKey2)
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

// 1. Test that the consul KV config is transferred correctly
// 2. Verify that an update is reflected in the config object
func (cs *ConfiguratorTestSuite) TestKVInit() {
	var err error

	cs.buildKVTestData()

	c := &config{
		t: cs.T(),
	}
	cm := cs.client.NewConfigManager(c)

	err = cm.AddKVPrefix(configuratorPrefix)
	require.Nil(cs.T(), err, "AddKVPrefix(%s) failed: %s", configuratorPrefix, err)

	// time.Sleep(time.Second)

	cs.T().Logf("cm=%+v", cm)
	c = cm.Refresh().Read().(*config)

	// Check that config has what we expect
	require.Equal(cs.T(), configuratorVal1, c.var1, "the initialized val1 is not what I expected")
	require.Equal(cs.T(), configuratorVal2, c.var2, "the initialized val2 is not what I expected")

	// Change the kv:s in consul
	cs.T().Log("=== changing the kv values")
	ch := cm.Subscribe()
	kv1 := &api.KVPair{Key: configuratorPrefix + configuratorKey1, Value: []byte(configuratorVal1b)}
	_, err = cs.client.KV().Put(kv1, nil)
	require.Nil(cs.T(), err, "Trouble changing the value of %s", configuratorKey1)
	c = (<-*ch).(*config)
	require.Equal(cs.T(), configuratorVal1b, c.var1, "var1 is not what i expected after updating in consul")

	kv2 := &api.KVPair{Key: configuratorPrefix + configuratorKey2, Value: []byte(fmt.Sprintf("%d", configuratorVal2b))}
	_, err = cs.client.KV().Put(kv2, nil)
	require.Nil(cs.T(), err, "Trouble changing the value of %s", configuratorKey2)
	c = (<-*ch).(*config)
	require.Equal(cs.T(), configuratorVal2b, c.var2, "var2 is not what i expected after updating in consul")

	// report what is actually in the kv prefix now:
	kvps, _, err := cs.client.KV().List(configuratorPrefix, nil)
	c.Update(0, kvps)
	cs.T().Logf("config after manual update: %+v", c)

	cm.Stop()
}

// 1. Start a web service that provides a health check
// 2. Register the service with consul
// 3. Verify that we can see the service before it starts passing it's health check
// 4. Observe the service as it starts passing in consul.
func (cs *ConfiguratorTestSuite) TestServiceInit() {

	cs.buildTestService()

	c := &config{
		t: cs.T(),
	}
	cm := cs.client.NewConfigManager(c)

	err := cm.AddService(configuratorServiceName, configuratorServiceTag1, false)
	require.Nil(cs.T(), err, "AddKVPrefix(%s) failed: %s", configuratorPrefix, err)

	time.Sleep(time.Second)

	cs.T().Logf("cm=%+v", cm)

	c = cm.Read().(*config)
	require.Equal(cs.T(), 1, len(c.service), "Expecting exactly one service here")

	// List the health checks before the service can be expected to pass
	se := c.service[0]
	for _, check := range se.Checks {
		cs.T().Logf("check[%s]=%s", check.Name, check.Status)
	}

	// Wait for consul to do a health check
	time.Sleep(10 * time.Second)

	// The service should be passing now
	c = cm.Read().(*config)
	require.Equal(cs.T(), 1, len(c.service), "Expecting exactly one service here")

	se = c.service[0]
	for _, check := range se.Checks {
		cs.T().Logf("check[%s]=%s", check.Name, check.Status)
	}

	// Clean up
	cm.Stop()
}

// buildKVTestData populates a kv path with some data
func (cs *ConfiguratorTestSuite) buildKVTestData() {
	var err error

	kv1 := &api.KVPair{Key: configuratorPrefix + configuratorKey1, Value: []byte(configuratorVal1)}
	_, err = cs.client.KV().Put(kv1, nil)
	require.Nil(cs.T(), err, "Failed storing key1/val1: %s", err)

	kv2 := &api.KVPair{Key: configuratorPrefix + configuratorKey2, Value: []byte(fmt.Sprintf("%d", configuratorVal2))}
	_, err = cs.client.KV().Put(kv2, nil)
	require.Nil(cs.T(), err, "Failed storing key2/val2: %s", err)
}

// create a consul service to test with
func (cs *ConfiguratorTestSuite) buildTestService() {
	// Fire up a simple health check
	portString := fmt.Sprintf(":%d", configuratorServicePort)
	http.HandleFunc(configuratorServicePath, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "Hello")
	})
	go http.ListenAndServe(portString, nil)

	// Register a service
	sr := &consultant.SimpleServiceRegistration{
		Name:      configuratorServiceName,
		Port:      configuratorServicePort,
		Address:   "localhost",
		RandomID:  true,
		Tags:      []string{configuratorServiceTag1, configuratorServiceTag2},
		CheckPath: configuratorServicePath,
		Interval:  "5s",
	}
	serviceID, err := cs.client.SimpleServiceRegister(sr)
	require.Nil(cs.T(), err, "Trouble registering the test service")
	cs.T().Logf("serviceID=%s", serviceID)
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
	cs.T().Log("TearDownSuite()")
	cs.TearDownTest()
}

func ExampleConfigurator() {
	// Create a config struct
	// type config struct {
	//	kvVar string // Something we know how to update give a consul watch plan update
	//	otherVar int // Another config item
	//	service []*api.ServiceEntry // A service entry list for services in our consul environment
	// }

	// Assume func (c *config) Update(uint64, interface{}) is defined so that *config implements Configurator

	c := &config{
		otherVar: 123,
	}

	client,_ := consultant.NewDefaultClient()
	cm := client.NewConfigManager(c)

	cm.AddKVPrefix("apps/myapp")
	cm.AddService("elastic", "myapp", true)

	// Force a refresh and read the config
	c = cm.Refresh().Read().(*config)

	// Casual read of the config, get what is there at the moment
	c = cm.Read().(*config)

	update := cm.Subscribe()

	for {
		select {
		//BUG go test does not recognize the expression `case c = (<- *update).(*config):`
		case x := <- *update:
			c = x.(*config)
			// we have an up-to date config
		}
	}
}
