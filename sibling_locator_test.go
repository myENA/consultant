package consultant_test

import (
	"testing"

	"time"

	"github.com/hashicorp/consul/api"
	"github.com/myENA/consultant"
)

const (
	siblingLocatorClusterCount = 3

	quorumWaitDuration = 10 * time.Second

	siblingLocatorServiceName = "locator-test-service"
	siblingLocatorServiceAddr = "127.0.0.1"
)

func TestSiblingLocator(t *testing.T) {
	c := makeCluster(t, siblingLocatorClusterCount)
	defer c.shutdown()

	locators := make([]*consultant.SiblingLocator, siblingLocatorClusterCount)

	t.Run("register services", func(t *testing.T) {
		for i := 0; i < siblingLocatorClusterCount; i++ {
			err := c.client(i).Agent().ServiceRegister(&api.AgentServiceRegistration{
				Name:    siblingLocatorServiceName,
				Address: siblingLocatorServiceAddr,
				Port:    randomPort(),
			})

			if nil != err {
				t.Logf("Unable to register service \"%d\": %v", i, err)
				t.FailNow()
			}
		}
	})

	// TODO: Don't use a sleep...?
	t.Logf("\nWaiting around \"%d\" for quorum....\n", int64(quorumWaitDuration))
	time.Sleep(quorumWaitDuration)

	t.Run("create locators", func(t *testing.T) {
		for i := 0; i < siblingLocatorClusterCount; i++ {
			svcs, _, err := c.client(i).Catalog().Service(siblingLocatorServiceName, "", nil)
			if nil != err {
				t.Logf("Unable to locate services in node \"%d\": %v", i, err)
				t.FailNow()
			}

			if siblingLocatorClusterCount != len(svcs) {
				t.Logf("Expected to see \"%d\" services, saw \"%d\"", siblingLocatorClusterCount, len(svcs))
				t.FailNow()
			}

			for _, svc := range svcs {
				if svc.Node == c.server(i).Config.NodeName && svc.ServiceName == siblingLocatorServiceName {
					var err error
					locators[i], err = consultant.NewSiblingLocatorWithCatalogService(c.client(i), svc)
					if nil != err {
						t.Logf("Unable to create locator for node \"%d\": %v", i, err)
						t.FailNow()
					}
				}
			}

			if nil == locators[i] {
				t.Logf("Unable to locate service for node \"%s\"", c.server(i).Config.NodeName)
				t.FailNow()
			}
		}
	})


}
