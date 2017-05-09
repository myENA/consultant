package consultant_test

import (
	"fmt"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/testutil"
	"github.com/myENA/consultant"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"reflect"
	"testing"
)

const (
	clientTestKVKey   = "consultant/tests/junkey"
	clientTestKVValue = "i don't know what i'm doing"
)

type ClientTestSuite struct {
	suite.Suite

	server *testutil.TestServer
	client *consultant.Client
}

func TestClient(t *testing.T) {
	suite.Run(t, &ClientTestSuite{})
}

func (cs *ClientTestSuite) SetupTest() {
	cs.client, cs.server = makeServerAndConsultantClient(cs.T(), nil)
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

func (cs *ClientTestSuite) TestSimpleClientInteraction() {
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
