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

const (
	candidateLockKey = "consultant/tests/candidate-lock"
	candidateLockTTL = "5s"
)

type CandidateTestSuite struct {
	suite.Suite

	// these values are cyclical, and should be re-defined per test method
	server *testutil.TestServer
	client *consultant.Client
}

func TestCandidate(t *testing.T) {
	suite.Run(t, &CandidateTestSuite{})
}

// SetupTest is called before each method is run.
func (cs *CandidateTestSuite) SetupTest() {
	cs.server, cs.client = makeServerAndClient(cs.T(), nil)
}

// TearDownTest is called after each method has been run.
func (cs *CandidateTestSuite) TearDownTest() {
	if nil != cs.client {
		cs.client = nil
	}
	if nil != cs.server {
		// TODO: Stop seems to return an error when the process is killed...
		cs.server.Stop()
		cs.server = nil
	}
}

func (cs *CandidateTestSuite) TearDownSuite() {
	cs.TearDownTest()
}

func (cs *CandidateTestSuite) makeCandidate(num int) *consultant.Candidate {
	candidate, err := consultant.NewCandidate(cs.client, fmt.Sprintf("test-%d", num), candidateLockKey, candidateLockTTL)
	if nil != err {
		cs.T().Fatalf("err: %v", err)
	}

	return candidate
}

func (cs *CandidateTestSuite) TestSimpleElectionCycle() {
	var candidate1, candidate2, candidate3, leaderCandidate *consultant.Candidate
	var leader *api.SessionEntry
	var err error

	wg := new(sync.WaitGroup)

	wg.Add(3)

	go func() {
		candidate1 = cs.makeCandidate(1)
		candidate1.Wait()
		wg.Done()
	}()
	go func() {
		candidate2 = cs.makeCandidate(2)
		candidate2.Wait()
		wg.Done()
	}()
	go func() {
		candidate3 = cs.makeCandidate(3)
		candidate3.Wait()
		wg.Done()
	}()

	wg.Wait()

	leader, err = candidate1.LeaderService()
	require.Nil(cs.T(), err, fmt.Sprintf("Unable to locate leader session entry: %v", err))

	// attempt to locate elected leader
	switch leader.ID {
	case candidate1.SessionID():
		leaderCandidate = candidate1
	case candidate2.SessionID():
		leaderCandidate = candidate2
	case candidate3.SessionID():
		leaderCandidate = candidate3
	}

	require.NotNil(
		cs.T(),
		leaderCandidate,
		fmt.Sprintf(
			"Expected one of \"%+v\", saw \"%s\"",
			[]string{candidate1.SessionID(), candidate2.SessionID(), candidate3.SessionID()},
			leader.ID))

	leadersFound := 0
	for i, candidate := range []*consultant.Candidate{candidate1, candidate2, candidate3} {
		if leaderCandidate == candidate {
			leadersFound = 1
			continue
		}

		require.True(
			cs.T(),
			0 == leadersFound || 1 == leadersFound,
			fmt.Sprintf("leaderCandidate matched to more than 1 candidate.  Iteration \"%d\"", i))

		require.False(
			cs.T(),
			candidate.Elected(),
			fmt.Sprintf("Candidate \"%d\" is not elected but says that it is...", i))
	}

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

	leader, err = candidate1.LeaderService()
	require.NotNil(cs.T(), err, "Expected empty key error, got nil")
	require.Nil(cs.T(), leader, fmt.Sprintf("Expected nil leader, got %v", leader))
}
