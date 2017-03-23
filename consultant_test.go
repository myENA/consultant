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

func makeClient(t *testing.T) (*api.Client, *testutil.TestServer) {
	conf := api.DefaultConfig()

	server := testutil.NewTestServerConfig(t, nil)
	conf.Address = server.HTTPAddr

	client, err := api.NewClient(conf)
	if err != nil {
		server.Stop()
		t.Logf("err: %v", err)
		t.FailNow()
	}

	return client, server
}
