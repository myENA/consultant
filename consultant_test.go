package consultant_test

import (
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/testutil"
	"github.com/myENA/consultant"
	"math/rand"
	"net"
	"sync"
	"testing"
)

func init() {
	consultant.Debug()
}

func makeServerAndClient(t *testing.T, cb testutil.ServerConfigCallback) (*testutil.TestServer, *consultant.Client) {
	server, err := testutil.NewTestServerConfig(cb)
	if nil != err {
		t.Fatalf("Unable to initialize Consul agent server: %v", err)
	}

	apiConf := api.DefaultConfig()
	apiConf.Address = server.HTTPAddr

	client, err := consultant.NewClient(apiConf)
	if err != nil {
		server.Stop()
		t.Fatalf("Unable to create client for server \"%s\": %v", apiConf.Address, err)
	}

	return server, client
}

type testConsulCluster struct {
	lock sync.RWMutex

	size    int
	servers []*testutil.TestServer
	clients []*consultant.Client
}

func (c *testConsulCluster) client(node int) *consultant.Client {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.clients[node]
}

func (c *testConsulCluster) server(node int) *testutil.TestServer {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.servers[node]
}

func (c *testConsulCluster) randomClient() *consultant.Client {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.clients[rand.Intn(len(c.clients)-1)]
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
	if 0 > nodeCount {
		t.Fatalf("nodeCount must be > 0, \"%d\" provided", nodeCount)
	}

	c := &testConsulCluster{
		size:    nodeCount,
		servers: make([]*testutil.TestServer, nodeCount),
		clients: make([]*consultant.Client, nodeCount),
	}

	for i := 0; i < nodeCount; i++ {
		c.servers[i], c.clients[i] = makeServerAndClient(t, func(c *testutil.TestServerConfig) {
			c.Performance.RaftMultiplier = 1
			c.DisableCheckpoint = false
			if 0 < i {
				c.Bootstrap = false
			}
		})
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
