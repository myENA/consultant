package consultant_test

import (
	"fmt"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/testutil"
	"github.com/myENA/consultant"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"sync"
	"testing"
)

type CandidateTestSuite struct {
	suite.Suite

	// these values are cyclical, and should be re-defined per test method
	client *api.Client
	server *testutil.TestServer
}

func TestCandidate(t *testing.T) {
	suite.Run(t, &CandidateTestSuite{})
}

// SetupTest is called before each method is run.
func (cts *CandidateTestSuite) SetupTest() {
	var err error
	cts.client, cts.server, err = makeClientAndServer(cts.T(), nil)
	if nil != err {
		cts.FailNow(fmt.Sprintf("Unable to set up Consul Client and Server: %v", err))
	}
}

// TearDownTest is called after each method has been run.
func (cts *CandidateTestSuite) TearDownTest() {
	if nil != cts.client {
		cts.client = nil
	}
	if nil != cts.server {
		// TODO: Stop seems to return an error when the process is killed...
		cts.server.Stop()
		cts.server = nil
	}
}

func (cts *CandidateTestSuite) TeardDownSuite() {
	cts.TearDownTest()
}

func (cts *CandidateTestSuite) makeCandidate(num int) *consultant.Candidate {
	candidate, err := consultant.NewCandidate(cts.client, fmt.Sprintf("test-%d", num), "consultant/tests/candidate-lock", "5s")
	if nil != err {
		cts.T().Fatalf("err: %v", err)
	}

	return candidate
}

func (cts *CandidateTestSuite) TestSimpleElectionCycle() {
	var candidate1, candidate2, candidate3 *consultant.Candidate
	var leader *api.SessionEntry
	var err error

	wg := new(sync.WaitGroup)

	wg.Add(3)

	go func() {
		candidate1 = cts.makeCandidate(1)
		candidate1.Wait()
		wg.Done()
	}()
	go func() {
		candidate2 = cts.makeCandidate(2)
		candidate2.Wait()
		wg.Done()
	}()
	go func() {
		candidate3 = cts.makeCandidate(3)
		candidate3.Wait()
		wg.Done()
	}()

	wg.Wait()

	leader, err = candidate1.Leader()
	require.Nil(cts.T(), err, fmt.Sprintf("Unable to locate leader session entry: %v", err))

	require.True(
		cts.T(),
		leader.ID == candidate1.SessionID() ||
			leader.ID == candidate2.SessionID() ||
			leader.ID == candidate3.SessionID(),
		fmt.Sprintf(
			"Expected one of \"%v\", saw \"%s\"",
			[]string{candidate1.SessionID(), candidate2.SessionID(), candidate3.SessionID()},
			leader.ID))

	wg.Add(3)

	go func() {
		candidate1.Resign()
		wg.Done()
	}()
	go func() {
		candidate2.Resign()
		wg.Done()
	}()
	go func() {
		candidate3.Resign()
		wg.Done()
	}()

	wg.Wait()

	leader, err = candidate1.Leader()
	require.NotNil(cts.T(), err, "Expected empty key error, got nil")
	require.Nil(cts.T(), leader, fmt.Sprintf("Expected nil leader, got %v", leader))
}
