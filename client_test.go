package consultant_test

import (
	"fmt"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/testutil"
	"github.com/myENA/consultant"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"os"
	"reflect"
	"testing"
)

const (
	clientTestKVKey   = "consultant/tests/junkey"
	clientTestKVValue = "i don't know what i'm doing"

	clientSimpleServiceRegistrationName = "test-service"
	clientSimpleServiceRegistrationPort = 1234
)

type ClientTestSuite struct {
	suite.Suite

	server *testutil.TestServer
	client *consultant.Client
}

func TestClient(t *testing.T) {
	suite.Run(t, &ClientTestSuite{})
}

func (cs *ClientTestSuite) TearDownTest() {
	if nil != cs.server {
		cs.server.Stop()
		cs.server = nil
	}
	if nil != cs.client {
		cs.client = nil
	}
}

func (cs *ClientTestSuite) TearDownSuite() {
	cs.TearDownTest()
}

func (cs *ClientTestSuite) TestClientConstructionMethods() {
	var err error

	cs.server = makeServer(cs.T(), nil)

	os.Setenv(api.HTTPAddrEnvName, cs.server.HTTPAddr)

	_, err = consultant.NewClient(nil)
	require.NotNil(cs.T(), err, "Did not see an error when passing \"nil\" to consultant.NewCient()")

	_, err = consultant.NewDefaultClient()
	require.Nil(cs.T(), err, fmt.Sprintf("Saw error when attmepting to construct default client: %s", err))

	os.Unsetenv(api.HTTPAddrEnvName)
}

func (cs *ClientTestSuite) TestSimpleClientInteraction() {
	cs.server, cs.client = makeServerAndClient(cs.T(), nil)

	_, err := cs.client.KV().Put(&api.KVPair{Key: clientTestKVKey, Value: []byte(clientTestKVValue)}, nil)
	require.Nil(cs.T(), err, fmt.Sprintf("Unable to put key \"%s\": %s", clientTestKVKey, err))

	kv, _, err := cs.client.KV().Get(clientTestKVKey, nil)
	require.Nil(cs.T(), err, fmt.Sprintf("Unable to get key \"%s\": %s", clientTestKVKey, err))

	require.NotNil(cs.T(), kv, "KV was nil")
	require.IsType(
		cs.T(),
		&api.KVPair{},
		kv,
		fmt.Sprintf(
			"Expected KV Get response to be type \"%s\", saw \"%s\"",
			reflect.TypeOf(&api.KVPair{}),
			reflect.TypeOf(kv)))
}

func (cs *ClientTestSuite) TestSimpleServiceRegister() {
	cs.server, cs.client = makeServerAndClient(cs.T(), nil)

	reg := &consultant.SimpleServiceRegistration{
		Name: clientSimpleServiceRegistrationName,
		Port: clientSimpleServiceRegistrationPort,
	}

	sid, err := cs.client.SimpleServiceRegister(reg)
	require.Nil(cs.T(), err, fmt.Sprintf("Unable to utilize simple service registration: %s", err))

	svcs, _, err := cs.client.Health().Service(clientSimpleServiceRegistrationName, "", false, nil)
	require.Nil(cs.T(), err, fmt.Sprintf("Unable to locate service with name \"%s\": %s", clientSimpleServiceRegistrationName, err))

	sidList := make([]string, len(svcs))

	for i, s := range svcs {
		sidList[i] = s.Service.ID
	}

	require.Contains(cs.T(), sidList, sid, fmt.Sprintf("Expected to see service id \"%s\" in list \"%+v\"", sid, sidList))
}

func (cs *ClientTestSuite) TestGetServiceAddress() {
	cs.server, cs.client = makeServerAndClient(cs.T(), nil)

	reg := &consultant.SimpleServiceRegistration{
		Name: clientSimpleServiceRegistrationName,
		Port: clientSimpleServiceRegistrationPort,
	}

	_, err := cs.client.SimpleServiceRegister(reg)
	require.Nil(cs.T(), err, fmt.Sprintf("Unable to utilize simple service registration: %s", err))

	url, err := cs.client.BuildServiceURL("http", clientSimpleServiceRegistrationName, "", false, nil)
	require.Nil(cs.T(), err, fmt.Sprintf("Error seen while getting service URL: %s", err))
	require.NotNil(cs.T(), url, fmt.Sprintf("URL was nil.  Saw: %+v", url))

	require.Equal(
		cs.T(),
		fmt.Sprintf("%s:%d", cs.client.MyAddr(), clientSimpleServiceRegistrationPort),
		url.Host,
		fmt.Sprintf(
			"Expected address \"%s:%d\", saw \"%s\"",
			cs.client.MyAddr(),
			clientSimpleServiceRegistrationPort,
			url.Host))
}
