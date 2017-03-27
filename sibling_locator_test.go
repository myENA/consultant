package consultant_test

import (
	"testing"

	"fmt"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/myENA/consultant"
)

const (
	quorumWaitDuration = 10 * time.Second

	siblingLocatorClusterCount = 3
	siblingLocatorServiceName  = "locator-test-service"
	siblingLocatorServiceAddr  = "127.0.0.1"
)

// TODO: Move cluster, service, and locator creation to discrete function

func TestSiblingLocator_Current(t *testing.T) {
	localCluster := makeCluster(t, siblingLocatorClusterCount)
	defer localCluster.shutdown()

	locators := make([]*consultant.SiblingLocator, siblingLocatorClusterCount)
	results := make([][]consultant.Siblings, siblingLocatorClusterCount)

	t.Run("register services", func(t *testing.T) {
		for i := 0; i < siblingLocatorClusterCount; i++ {
			err := localCluster.client(i).Agent().ServiceRegister(&api.AgentServiceRegistration{
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
	time.Sleep(quorumWaitDuration)

	t.Run("create locators", func(t *testing.T) {
		for i := 0; i < siblingLocatorClusterCount; i++ {
			svcs, _, err := localCluster.client(i).Catalog().Service(siblingLocatorServiceName, "", nil)
			if nil != err {
				t.Logf("Unable to locate services in node \"%d\": %v", i, err)
				t.FailNow()
			}

			if siblingLocatorClusterCount != len(svcs) {
				t.Logf("Expected to see \"%d\" services, saw \"%d\"", siblingLocatorClusterCount, len(svcs))
				t.FailNow()
			}

			for _, svc := range svcs {
				if svc.Node == localCluster.server(i).Config.NodeName && svc.ServiceName == siblingLocatorServiceName {
					var err error
					locators[i], err = consultant.NewSiblingLocatorWithCatalogService(localCluster.client(i), svc)
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
				t.Logf("Unable to locate service for node \"%s\"", localCluster.server(i).Config.NodeName)
				t.FailNow()
			}
		}
	})

	t.Run("fetch current", func(t *testing.T) {
		// spread the word
		for i := 0; i < siblingLocatorClusterCount; i++ {
			locators[i].Current(false, true, nil)
		}

		// wait for callback routines to finish...
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

func TestSiblingLocator_Watchers(t *testing.T) {
	localCluster := makeCluster(t, siblingLocatorClusterCount)

	locators := make([]*consultant.SiblingLocator, siblingLocatorClusterCount)
	results := make([][]consultant.Siblings, siblingLocatorClusterCount)

	defer func() {
		for i := 0; i < siblingLocatorClusterCount; i++ {
			locators[i].StopWatcher()
		}
		localCluster.shutdown()
	}()

	t.Run("register services", func(t *testing.T) {
		for i := 0; i < siblingLocatorClusterCount; i++ {
			err := localCluster.client(i).Agent().ServiceRegister(&api.AgentServiceRegistration{
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
	time.Sleep(quorumWaitDuration)

	t.Run("create locators", func(t *testing.T) {
		for i := 0; i < siblingLocatorClusterCount; i++ {
			svcs, _, err := localCluster.client(i).Catalog().Service(siblingLocatorServiceName, "", nil)
			if nil != err {
				t.Logf("Unable to locate services in node \"%d\": %v", i, err)
				t.FailNow()
			}

			if siblingLocatorClusterCount != len(svcs) {
				t.Logf("Expected to see \"%d\" services, saw \"%d\"", siblingLocatorClusterCount, len(svcs))
				t.FailNow()
			}

			for _, svc := range svcs {
				if svc.Node == localCluster.server(i).Config.NodeName && svc.ServiceName == siblingLocatorServiceName {
					var err error
					locators[i], err = consultant.NewSiblingLocatorWithCatalogService(localCluster.client(i), svc)
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
				t.Logf("Unable to locate service for node \"%s\"", localCluster.server(i).Config.NodeName)
				t.FailNow()
			}
		}
	})

	t.Run("start watchers", func(t *testing.T) {
		for i := 0; i < siblingLocatorClusterCount; i++ {
			err := locators[i].StartWatcher(
				false,
				fmt.Sprintf("%s:%d",
					localCluster.server(i).Config.Addresses.HTTP,
					localCluster.server(i).Config.Ports.HTTP))
			if nil != err {
				t.Logf("Failed to start node \"%d\" watcher; %v", i, err)
				t.FailNow()
			}
		}
	})

	// TODO: Don't use a sleep...?
	time.Sleep(quorumWaitDuration)

	t.Run("change services", func(t *testing.T) {
		locators[0].StopWatcher()
		err := localCluster.client(0).Agent().ServiceDeregister(siblingLocatorServiceName)
		if nil != err {
			t.Logf("Unable to deregister service on node \"1\": %v", err)
			t.FailNow()
		}

		// wait for callback routines to finish
		time.Sleep(5 * time.Second)

		if len(results[1]) != len(results[2]) {
			t.Logf(
				"Expected node 1 and 2 to have same result length, saw: \"%d\" \"%d\"",
				len(results[0]),
				len(results[2]))
			t.FailNow()
		}

		if len(results[0]) != len(results[1])-1 {
			t.Logf(
				"Expected node 0 results to be 1 less than node 1, saw: \"%d\" \"%d\"",
				len(results[0]),
				len(results[1]))
			t.FailNow()
		}
	})
}
