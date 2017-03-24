package consultant_test

import (
	"testing"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/testutil"
	"github.com/myENA/consultant"
)

func init() {
	consultant.Debug()
}

func makeClientAndServer(t *testing.T) (*api.Client, *testutil.TestServer) {
	apiConf := api.DefaultConfig()

	server := testutil.NewTestServerConfig(t, nil)
	apiConf.Address = server.HTTPAddr

	client, err := api.NewClient(apiConf)
	if err != nil {
		server.Stop()
		t.Logf("err: %v", err)
		t.FailNow()
	}

	return client, server
}

type cluster struct {
	servers []*testutil.TestServer
	clients []*api.Client
}

func (c *cluster) client(i int) *api.Client {
	return c.clients[i]
}

func (c *cluster) server(i int) *testutil.TestServer {
	return c.servers[i]
}

func makeCluster(t *testing.T, nodeCount int) *cluster {
	if 0 > nodeCount {
		t.Fatalf("nodeCount must be >= 0, \"%d\" provided", nodeCount)
	}

	c := &cluster{
		servers: make([]*testutil.TestServer, nodeCount),
		clients: make([]*api.Client, nodeCount),
	}

	for i := 0; i < nodeCount; i++ {
		c.clients[i], c.servers[i] = makeClientAndServer(t)
	}

	if 1 == nodeCount {
		return c
	}

	for i := 1; i < nodeCount; i++ {
		c.servers[i].JoinLAN(c.servers[0].LANAddr)
	}

	return c
}
