package consultant_test

import (
	"testing"

	"time"

	"github.com/hashicorp/consul/api"
	"github.com/myENA/consultant"
)

const (
	quorumWaitDuration = 5 * time.Second

	siblingLocatorClusterCount = 3
	siblingLocatorServiceName  = "locator-test-service"
	siblingLocatorServiceAddr  = "127.0.0.1"
)

func TestSiblingLocator(t *testing.T) {
	c := makeCluster(t, siblingLocatorClusterCount)
	defer c.shutdown()

	locators := make([]*consultant.SiblingLocator, siblingLocatorClusterCount)
	results := make([][]consultant.Siblings, siblingLocatorClusterCount)

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

					results[i] = make([]consultant.Siblings, 0)

					// :|
					func(i int, locator *consultant.SiblingLocator) {
						locator.AddCallback("", func(_ uint64, siblings consultant.Siblings) {
							results[i] = append(results[i], siblings)
						})
					}(i, locators[i])

				}
			}

			if nil == locators[i] {
				t.Logf("Unable to locate service for node \"%s\"", c.server(i).Config.NodeName)
				t.FailNow()
			}
		}
	})

	t.Run("fetch current", func(t *testing.T) {
		// spread the word
		for i := 0; i < siblingLocatorClusterCount; i++ {
			locators[i].Current(false, true, nil)
		}

		// wait for receiver threads to finish...
		time.Sleep(1 * time.Second)

		// Should only have 1 entry
		for i := 0; i < siblingLocatorClusterCount; i++ {
			if 1 != len(results[i]) {
				t.Logf("Expected \"results[%d]\" to be length 1, saw \"%d\"", i, len(results[i]))
				t.FailNow()
			}
		}

		for i := 0; i < siblingLocatorClusterCount; i++ {
			if siblingLocatorClusterCount-1 != len(results[i][0]) {
				t.Logf(
					"Expected node \"%d\" result \"0\" length \"%d\", saw \"%d\"",
					i,
					siblingLocatorClusterCount-1,
					len(results[i][0]))
				t.FailNow()
			}
		}
	})
}
