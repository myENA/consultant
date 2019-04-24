package testutil

import (
	"fmt"
	"github.com/hashicorp/consul/api"
	cst "github.com/hashicorp/consul/testutil"
	"github.com/myENA/consultant"
	"math/rand"
	"net"
	"sync"
	"testing"
)

func makeConfigCallback(cb cst.ServerConfigCallback) cst.ServerConfigCallback {
	return func(c *cst.TestServerConfig) {
		c.NodeName = fmt.Sprintf("%s-%s", nouns[rand.Int63()%nounLen], nouns[rand.Int63()%nounLen])
		c.LogLevel = "debug"
		if cb != nil {
			cb(c)
		}
	}
}

func MakeServer(t *testing.T, cb cst.ServerConfigCallback) *cst.TestServer {
	server, err := cst.NewTestServerConfigT(t, makeConfigCallback(cb))
	if err != nil {
		t.Fatalf("Unable to initialize Consul agent server: %v", err)
	}

	return server
}

func MakeClient(t *testing.T, server *cst.TestServer) *consultant.Client {
	apiConf := api.DefaultConfig()
	apiConf.Address = server.HTTPAddr

	client, err := consultant.NewClient(apiConf)
	if err != nil {
		if err := server.Stop(); err != nil {
			t.Logf("error shutting down server: %s", err)
		}
		t.Fatalf("Unable to create client for server \"%s\": %v", apiConf.Address, err)
	}

	return client
}

func MakeServerAndClient(t *testing.T, cb cst.ServerConfigCallback) (*cst.TestServer, *consultant.Client) {
	server := MakeServer(t, cb)
	return server, MakeClient(t, server)
}

type TestConsulCluster struct {
	mu sync.RWMutex

	size    int
	servers []*cst.TestServer
	clients []*consultant.Client
}

func (c *TestConsulCluster) Client(node int) *consultant.Client {
	c.mu.RLock()
	client := c.clients[node]
	c.mu.RUnlock()
	return client
}

func (c *TestConsulCluster) Server(node int) *cst.TestServer {
	c.mu.RLock()
	server := c.servers[node]
	c.mu.RUnlock()
	return server
}

func (c *TestConsulCluster) RandomClient() *consultant.Client {
	c.mu.RLock()
	client := c.clients[rand.Intn(len(c.clients))]
	c.mu.RUnlock()
	return client
}

func (c *TestConsulCluster) Shutdown() {
	c.mu.Lock()

	for _, serv := range c.servers {
		serv.Stop()
	}

	c.size = 0
	c.servers = make([]*cst.TestServer, 0)
	c.clients = make([]*consultant.Client, 0)

	c.mu.Unlock()
}

func MakeCluster(t *testing.T, nodeCount int) (*TestConsulCluster, error) {
	if 0 > nodeCount {
		t.Fatalf("nodeCount must be > 0, \"%d\" provided", nodeCount)
	}

	c := &TestConsulCluster{
		size:    nodeCount,
		servers: make([]*cst.TestServer, nodeCount),
		clients: make([]*consultant.Client, nodeCount),
	}

	for i := 0; i < nodeCount; i++ {
		c.servers[i], c.clients[i] = MakeServerAndClient(t, func(c *cst.TestServerConfig) {
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

// shamelessly copy-pasted from and old version of consul test util
// randomPort asks the kernel for a random port to use.
func RandomPort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
