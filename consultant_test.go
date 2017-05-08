package consultant_test

import (
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/testutil"
	"github.com/myENA/consultant"
	"net"
	"sync"
	"testing"
)

func init() {
	consultant.Debug()
}

func makeClientAndServer(t *testing.T, cb testutil.ServerConfigCallback) (*api.Client, *testutil.TestServer, error) {
	apiConf := api.DefaultConfig()

	server, err := testutil.NewTestServerConfig(cb)
	if nil != err {

	}
	apiConf.Address = server.HTTPAddr

	client, err := api.NewClient(apiConf)
	if err != nil {
		server.Stop()
		t.Logf("err: %v", err)
		t.FailNow()
	}

	return client, server, nil
}

type testConsulCluster struct {
	lock sync.RWMutex

	size    int
	servers []*testutil.TestServer
	clients []*api.Client
}

func (c *testConsulCluster) client(node int) *api.Client {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.clients[node]
}

func (c *testConsulCluster) server(node int) *testutil.TestServer {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.servers[node]
}

func (c *testConsulCluster) shutdown() {
	c.lock.Lock()
	defer c.lock.Unlock()

	for i := 0; i < c.size; i++ {
		c.servers[i].Stop()
	}

	c.size = 0
	c.servers = nil
	c.clients = nil
}

func makeCluster(t *testing.T, nodeCount int) (*testConsulCluster, error) {
	var err error
	if 0 > nodeCount {
		t.Fatalf("nodeCount must be >= 0, \"%d\" provided", nodeCount)
	}

	c := &testConsulCluster{
		size:    nodeCount,
		servers: make([]*testutil.TestServer, nodeCount),
		clients: make([]*api.Client, nodeCount),
	}

	for i := 0; i < nodeCount; i++ {
		c.clients[i], c.servers[i], err = makeClientAndServer(t, func(c *testutil.TestServerConfig) {
			c.Performance.RaftMultiplier = 5
			c.DisableCheckpoint = false
			if 0 < i {
				c.Bootstrap = false
			}
		})
		if nil != err {
			return nil, err
		}
	}

	if 1 == nodeCount {
		return c, nil
	}

	for i := 1; i < nodeCount; i++ {
		c.servers[0].JoinLAN(t, c.servers[i].LANAddr)
	}

	return c, nil
}

// shamelessly copy-pasted from https://github.com/hashicorp/consul/blob/master/testutil/server.go#L107
// randomPort asks the kernel for a random port to use.
func randomPort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
