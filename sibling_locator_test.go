package consultant_test

import (
	"fmt"
	"github.com/hashicorp/consul/api"
	"github.com/myENA/consultant"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"log"
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

	localCluster *testConsulCluster

	locators []*consultant.SiblingLocator
	results  [][]consultant.Siblings
}

func TestSiblingLocator_Current(t *testing.T) {
	suite.Run(t, &SiblingLocatorTestSuite{})
}

// SetupTest is run before each test method
func (sls *SiblingLocatorTestSuite) SetupTest() {
	var err error

	sls.localCluster, err = makeCluster(sls.T(), siblingLocatorClusterCount)
	if nil != err {
		log.Fatalf("Unable to initialize test consul cluster: %v", err)
	}

	sls.locators = make([]*consultant.SiblingLocator, siblingLocatorClusterCount)
	sls.results = make([][]consultant.Siblings, siblingLocatorClusterCount)
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
		sls.localCluster.shutdown()
		sls.localCluster = nil
	}
}

func (sls *SiblingLocatorTestSuite) TearDownSuite() {
	sls.TearDownTest()
}

func (sls *SiblingLocatorTestSuite) TestCurrent() {
	for i := 0; i < siblingLocatorClusterCount; i++ {
		err := sls.localCluster.client(i).Agent().ServiceRegister(&api.AgentServiceRegistration{
			Name:    siblingLocatorServiceName,
			Address: siblingLocatorServiceAddr,
			Port:    randomPort(),
		})

		require.Nil(sls.T(), err, fmt.Sprintf("Unable to register service \"%d\": %v", i, err))
	}

	// TODO: Don't use a sleep...?
	time.Sleep(quorumWaitDuration)

	for i := 0; i < siblingLocatorClusterCount; i++ {
		svcs, _, err := sls.localCluster.client(i).Catalog().Service(siblingLocatorServiceName, "", nil)
		require.Nil(sls.T(), err, fmt.Sprintf("Unable to locate services in node \"%d\": %v", i, err))

		require.Len(sls.T(), svcs, siblingLocatorClusterCount, fmt.Sprintf("Expected to see \"%d\" services, saw \"%d\"", siblingLocatorClusterCount, len(svcs)))

		for _, svc := range svcs {
			if svc.Node == sls.localCluster.server(i).Config.NodeName && svc.ServiceName == siblingLocatorServiceName {
				var err error
				sls.locators[i], err = consultant.NewSiblingLocatorWithCatalogService(sls.localCluster.client(i), svc)
				require.Nil(sls.T(), err, fmt.Sprintf("Unable to create locator for node \"%d\": %v", i, err))

				sls.results[i] = make([]consultant.Siblings, 0)

				// :|
				func(i int, locator *consultant.SiblingLocator) {
					locator.AddCallback("", func(_ uint64, siblings consultant.Siblings) {
						sls.results[i] = append(sls.results[i], siblings)
					})
				}(i, sls.locators[i])
			}
		}

		require.NotNil(sls.T(), sls.locators[i], fmt.Sprintf("Unable to locate service for node \"%s\"", sls.localCluster.server(i).Config.NodeName))
	}

	// spread the word
	for i := 0; i < siblingLocatorClusterCount; i++ {
		sls.locators[i].Current(false, true)
	}

	// wait for callback routines to finish...
	time.Sleep(1 * time.Second)

	// Should only have 1 entry
	for i := 0; i < siblingLocatorClusterCount; i++ {
		require.Len(sls.T(), sls.results[i], 1, fmt.Sprintf("Expected \"results[%d]\" to be length 1, saw \"%d\"", i, len(sls.results[i])))
	}

	for i := 0; i < siblingLocatorClusterCount; i++ {
		require.Len(sls.T(), sls.results[i][0], siblingLocatorClusterCount-1, fmt.Sprintf(
			"Expected node \"%d\" result \"0\" length \"%d\", saw \"%d\"",
			i,
			siblingLocatorClusterCount-1,
			len(sls.results[i][0])))
	}
}

func (sls *SiblingLocatorTestSuite) TestWatchers() {
	for i := 0; i < siblingLocatorClusterCount; i++ {
		err := sls.localCluster.client(i).Agent().ServiceRegister(&api.AgentServiceRegistration{
			Name:    siblingLocatorServiceName,
			Address: siblingLocatorServiceAddr,
			Port:    randomPort(),
		})

		require.Nil(sls.T(), err, fmt.Sprintf("Unable to register service \"%d\": %v", i, err))
	}

	// TODO: Don't use a sleep...?
	time.Sleep(quorumWaitDuration)

	for i := 0; i < siblingLocatorClusterCount; i++ {
		svcs, _, err := sls.localCluster.client(i).Catalog().Service(siblingLocatorServiceName, "", nil)
		require.Nil(sls.T(), err, fmt.Sprintf("Unable to locate services in node \"%d\": %v", i, err))

		require.Len(sls.T(), svcs, siblingLocatorClusterCount, fmt.Sprintf("Expected to see \"%d\" services, saw \"%d\"", siblingLocatorClusterCount, len(svcs)))

		for _, svc := range svcs {
			if svc.Node == sls.localCluster.server(i).Config.NodeName && svc.ServiceName == siblingLocatorServiceName {
				var err error
				sls.locators[i], err = consultant.NewSiblingLocatorWithCatalogService(sls.localCluster.client(i), svc)
				require.Nil(sls.T(), err, fmt.Sprintf("Unable to create locator for node \"%d\": %v", i, err))

				sls.results[i] = make([]consultant.Siblings, 0)

				// :|
				func(i int, locator *consultant.SiblingLocator) {
					locator.AddCallback("", func(_ uint64, siblings consultant.Siblings) {
						sls.results[i] = append(sls.results[i], siblings)
					})
				}(i, sls.locators[i])

			}
		}

		require.NotNil(sls.T(), sls.locators[i], fmt.Sprintf("Unable to locate service for node \"%s\"", sls.localCluster.server(i).Config.NodeName))
	}

	for i := 0; i < siblingLocatorClusterCount; i++ {
		err := sls.locators[i].StartWatcher(
			false,
			fmt.Sprintf("%s:%d",
				sls.localCluster.server(i).Config.Addresses.HTTP,
				sls.localCluster.server(i).Config.Ports.HTTP))

		require.Nil(sls.T(), err, fmt.Sprintf("Failed to start node \"%d\" watcher; %v", i, err))
	}

	// wait for watcher routines to start up
	time.Sleep(quorumWaitDuration)

	sls.locators[0].StopWatcher()
	err := sls.localCluster.client(0).Agent().ServiceDeregister(siblingLocatorServiceName)
	require.Nil(sls.T(), err, fmt.Sprintf("Unable to deregister service on node \"1\": %v", err))

	// wait for quorum service deregistration and watcher callbacks to finish
	time.Sleep(quorumWaitDuration)

	require.Len(
		sls.T(),
		sls.results[1],
		len(sls.results[2]),
		fmt.Sprintf(
			"Expected node 1 and 2 to have same result length, saw: \"%d\" \"%d\"",
			len(sls.results[0]),
			len(sls.results[2])))

	require.Len(
		sls.T(),
		sls.results[0],
		len(sls.results[1])-1,
		fmt.Sprintf(
			"Expected node 0 results to be 1 less than node 1, saw: \"%d\" \"%d\"",
			len(sls.results[0]),
			len(sls.results[1])))
}
