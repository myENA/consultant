package session_test

import (
	"github.com/hashicorp/consul/api"
	cst "github.com/hashicorp/consul/testutil"
	"github.com/myENA/consultant/candidate/session"
	"github.com/myENA/consultant/testutil"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"testing"
	"time"
)

type UtilTestSuite struct {
	suite.Suite

	server *cst.TestServer
	client *api.Client

	session *session.Session
}

func TestSessionUtil(t *testing.T) {
	suite.Run(t, &UtilTestSuite{})
}

func (us *UtilTestSuite) SetupSuite() {
	server, client := testutil.MakeServerAndClient(us.T(), nil)
	us.server = server
	us.client = client.Client
}

func (us *UtilTestSuite) TearDownSuite() {
	if us.session != nil {
		us.session.Stop()
	}
	us.session = nil
	if us.server != nil {
		us.server.Stop()
	}
	us.server = nil
	us.client = nil
}

func (us *UtilTestSuite) TearDownTest() {
	if us.session != nil {
		us.session.Stop()
	}
	us.session = nil
}

func (us *UtilTestSuite) TestParseName_NoKey() {
	var err error

	updated := make(chan session.Update, 1)
	updater := func(up session.Update) {
		updated <- up
	}

	us.session, err = session.New(&session.Config{
		Client:     us.client,
		TTL:        "10s",
		UpdateFunc: updater,
	})
	require.Nil(us.T(), err, "Error creating session: %s", err)

	us.session.Run()

	var up session.Update

	select {
	case <-time.After(10 * time.Second):
		us.FailNow("Expected to receive session update by now")
	case up = <-updated:
		require.Nil(us.T(), up.Error, "Session update error: %s", err)
	}

	name := us.session.Name()
	require.NotZero(us.T(), name, "Expected name to be populated")

	parsed, err := session.ParseName(name)
	require.Nil(us.T(), err, "Error parsing session name: %s", err)

	require.Zero(us.T(), parsed.Key, "Expected Key to be empty: %+v", parsed)
	require.NotZero(us.T(), parsed.NodeName, "Expected NodeName to be populated: %+v", parsed)
	require.NotZero(us.T(), parsed.RandomID, "Expected RandomID to be populated: %+v", parsed)

	node, err := us.client.Agent().NodeName()
	require.Nil(us.T(), err, "Unable to fetch node: %s", err)

	require.Equal(us.T(), node, parsed.NodeName, "Expected NodeName to be \"%s\", saw \"%s\"", node, parsed.NodeName)
}

func (us *UtilTestSuite) TestParseName_Keyed() {
	var err error

	updated := make(chan session.Update, 1)
	updater := func(up session.Update) {
		updated <- up
	}

	us.session, err = session.New(&session.Config{
		Key:        testKey,
		Client:     us.client,
		TTL:        "10s",
		UpdateFunc: updater,
	})
	require.Nil(us.T(), err, "Error creating session: %s", err)

	us.session.Run()

	var up session.Update

	select {
	case <-time.After(10 * time.Second):
		us.FailNow("Expected to receive session update by now")
	case up = <-updated:
		require.Nil(us.T(), up.Error, "Session update error: %s", err)
	}

	name := us.session.Name()
	require.NotZero(us.T(), name, "Expected name to be populated")

	parsed, err := session.ParseName(name)
	require.Nil(us.T(), err, "Error parsing session name: %s", err)

	require.NotZero(us.T(), parsed.Key, "Expected Key to be populated: %+v", parsed)
	require.NotZero(us.T(), parsed.NodeName, "Expected NodeName to be populated: %+v", parsed)
	require.NotZero(us.T(), parsed.RandomID, "Expected RandomID to be populated: %+v", parsed)

	node, err := us.client.Agent().NodeName()
	require.Nil(us.T(), err, "Unable to fetch node: %s", err)

	require.Equal(us.T(), testKey, parsed.Key, "Expected Key to equal \"%s\", but saw \"%s\"", testKey, parsed.Key)
	require.Equal(us.T(), node, parsed.NodeName, "Expected NodeName to be \"%s\", saw \"%s\"", node, parsed.NodeName)
}
