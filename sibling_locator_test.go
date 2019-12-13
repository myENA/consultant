package consultant_test

import (
	"fmt"
	"github.com/hashicorp/consul/api"

	"github.com/myENA/consultant/v2"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"log"
	"sync"
	"testing"
	"time"
)

const (
	quorumWaitDuration = 10 * time.Second

	siblingLocatorClusterCount = 3
	siblingLocatorServiceName  = "locator-test-service"
	siblingLocatorServiceAddr  = "127.0.0.1"
)

type SiblingLocatorTestSuite struct {
	suite.Suite

	localCluster *testutil.TestConsulCluster

	locators []*consultant.SiblingLocator
}

func TestSiblingLocator_Current(t *testing.T) {
	suite.Run(t, &SiblingLocatorTestSuite{})
}

// SetupTest is run before each test method
func (sls *SiblingLocatorTestSuite) SetupTest() {
	var err error

	sls.localCluster, err = testutil.MakeCluster(sls.T(), siblingLocatorClusterCount)
	if err != nil {
		log.Fatalf("Unable to initialize test consul cluster: %v", err)
	}

	sls.locators = make([]*consultant.SiblingLocator, siblingLocatorClusterCount)
}

// TearDownTest is run after each test method
func (sls *SiblingLocatorTestSuite) TearDownTest() {
	if nil != sls.locators && 0 < len(sls.locators) {
		for i := 0; i < siblingLocatorClusterCount; i++ {
			if nil != sls.locators[i] {
				sls.locators[i].StopWatcher()
			}
		}

		sls.locators = nil
	}

	if nil != sls.localCluster {
		sls.localCluster.Shutdown()
		sls.localCluster = nil
	}
}

func (sls *SiblingLocatorTestSuite) TearDownSuite() {
	sls.TearDownTest()
}

func (sls *SiblingLocatorTestSuite) TestCurrent() {
	resultsMu := sync.Mutex{}
	results := make([][]consultant.Siblings, siblingLocatorClusterCount)

	for i := 0; i < siblingLocatorClusterCount; i++ {
		err := sls.localCluster.Client(i).Agent().ServiceRegister(&api.AgentServiceRegistration{
			Name:    siblingLocatorServiceName,
			Address: siblingLocatorServiceAddr,
			Port:    testutil.RandomPort(),
		})

		require.Nil(sls.T(), err, fmt.Sprintf("Unable to register service \"%d\": %v", i, err))
	}

	// TODO: Don't use a sleep...?
	time.Sleep(quorumWaitDuration)

	for i := 0; i < siblingLocatorClusterCount; i++ {
		svcs, _, err := sls.localCluster.Client(i).Catalog().Service(siblingLocatorServiceName, "", nil)
		require.Nil(sls.T(), err, fmt.Sprintf("Unable to locate services in node \"%d\": %v", i, err))

		require.Len(sls.T(), svcs, siblingLocatorClusterCount, fmt.Sprintf("Expected to see \"%d\" services, saw \"%d\"", siblingLocatorClusterCount, len(svcs)))

		for _, svc := range svcs {
			if svc.Node == sls.localCluster.Server(i).Config.NodeName && svc.ServiceName == siblingLocatorServiceName {
				var err error
				sls.locators[i], err = consultant.NewSiblingLocatorWithCatalogService(sls.localCluster.Client(i), svc)
				require.Nil(sls.T(), err, fmt.Sprintf("Unable to create locator for node \"%d\": %v", i, err))

				resultsMu.Lock()
				results[i] = make([]consultant.Siblings, 0)
				resultsMu.Unlock()

				// :|
				func(i int, locator *consultant.SiblingLocator) {
					locator.AddCallback("", func(_ uint64, siblings consultant.Siblings) {
						resultsMu.Lock()
						results[i] = append(results[i], siblings)
						resultsMu.Unlock()
					})
				}(i, sls.locators[i])
			}
		}

		require.NotNil(sls.T(), sls.locators[i], fmt.Sprintf("Unable to locate service for node \"%s\"", sls.localCluster.Server(i).Config.NodeName))
	}

	// spread the word
	for i := 0; i < siblingLocatorClusterCount; i++ {
		sls.locators[i].Current(false, true)
	}

	// wait for callback routines to finish...
	time.Sleep(1 * time.Second)

	// Should only have 1 entry
	for i := 0; i < siblingLocatorClusterCount; i++ {
		resultsMu.Lock()
		require.Len(sls.T(), results[i], 1, fmt.Sprintf("Expected \"results[%d]\" to be length 1, saw \"%d\"", i, len(results[i])))
		resultsMu.Unlock()
	}

	for i := 0; i < siblingLocatorClusterCount; i++ {
		resultsMu.Lock()
		require.Len(sls.T(), results[i][0], siblingLocatorClusterCount-1, fmt.Sprintf(
			"Expected node \"%d\" result \"0\" length \"%d\", saw \"%d\"",
			i,
			siblingLocatorClusterCount-1,
			len(results[i][0])))
		resultsMu.Unlock()
	}
}

func (sls *SiblingLocatorTestSuite) TestWatchers() {
	resultsMu := sync.Mutex{}
	results := make([][]consultant.Siblings, siblingLocatorClusterCount)

	for i := 0; i < siblingLocatorClusterCount; i++ {
		err := sls.localCluster.Client(i).Agent().ServiceRegister(&api.AgentServiceRegistration{
			Name:    siblingLocatorServiceName,
			Address: siblingLocatorServiceAddr,
			Port:    testutil.RandomPort(),
		})

		require.Nil(sls.T(), err, fmt.Sprintf("Unable to register service \"%d\": %v", i, err))
	}

	// TODO: Don't use a sleep...?
	time.Sleep(quorumWaitDuration)

	for i := 0; i < siblingLocatorClusterCount; i++ {
		svcs, _, err := sls.localCluster.Client(i).Catalog().Service(siblingLocatorServiceName, "", nil)
		require.Nil(sls.T(), err, fmt.Sprintf("Unable to locate services in node \"%d\": %v", i, err))

		require.Len(sls.T(), svcs, siblingLocatorClusterCount, fmt.Sprintf("Expected to see \"%d\" services, saw \"%d\"", siblingLocatorClusterCount, len(svcs)))

		for _, svc := range svcs {
			if svc.Node == sls.localCluster.Server(i).Config.NodeName && svc.ServiceName == siblingLocatorServiceName {
				var err error
				sls.locators[i], err = consultant.NewSiblingLocatorWithCatalogService(sls.localCluster.Client(i), svc)
				require.Nil(sls.T(), err, fmt.Sprintf("Unable to create locator for node \"%d\": %v", i, err))

				resultsMu.Lock()
				results[i] = make([]consultant.Siblings, 0)
				resultsMu.Unlock()

				// :|
				func(i int, locator *consultant.SiblingLocator) {
					locator.AddCallback("", func(_ uint64, siblings consultant.Siblings) {
						resultsMu.Lock()
						results[i] = append(results[i], siblings)
						resultsMu.Unlock()
					})
				}(i, sls.locators[i])

			}
		}

		require.NotNil(sls.T(), sls.locators[i], fmt.Sprintf("Unable to locate service for node \"%s\"", sls.localCluster.Server(i).Config.NodeName))
	}

	for i := 0; i < siblingLocatorClusterCount; i++ {
		err := sls.locators[i].StartWatcher(false)
		require.Nil(sls.T(), err, fmt.Sprintf("Failed to start node \"%d\" watcher; %v", i, err))
	}

	// wait for watcher routines to start up
	time.Sleep(quorumWaitDuration)

	sls.locators[0].StopWatcher()
	err := sls.localCluster.Client(0).Agent().ServiceDeregister(siblingLocatorServiceName)
	require.Nil(sls.T(), err, fmt.Sprintf("Unable to deregister service on node \"1\": %v", err))

	// wait for quorum service deregistration and watcher callbacks to finish
	time.Sleep(quorumWaitDuration)

	resultsMu.Lock()
	require.Len(
		sls.T(),
		results[1],
		len(results[2]),
		fmt.Sprintf(
			"Expected node 1 and 2 to have same result length, saw: \"%d\" \"%d\"",
			len(results[0]),
			len(results[2])))

	fmt.Print("\n\n\n\n\n", results, "\n\n\n\n\n")

	require.Len(
		sls.T(),
		results[0],
		len(results[1])-1,
		fmt.Sprintf(
			"Expected node 0 results to be 1 less than node 1, saw: \"%d\" \"%d\"",
			len(results[0]),
			len(results[1])))

	resultsMu.Unlock()
}
