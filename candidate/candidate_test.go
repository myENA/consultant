package candidate_test

import (
	"fmt"
	"github.com/hashicorp/consul/api"
	cst "github.com/hashicorp/consul/sdk/testutil"
	"github.com/myENA/consultant"
	"github.com/myENA/consultant/candidate"
	"github.com/myENA/consultant/testutil"
	"github.com/myENA/consultant/util"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"sync"
	"testing"
)

const (
	testKVKey = "consultant/test/candidate-test"
	testID    = "test-candidate"
	lockTTL   = "5s"
)

func init() {
	consultant.Debug()
}

type CandidateTestSuite struct {
	suite.Suite

	server *cst.TestServer
	client *api.Client
}

func TestCandidate(t *testing.T) {
	suite.Run(t, new(CandidateTestSuite))
}

func (cs *CandidateTestSuite) TearDownTest() {
	if cs.client != nil {
		cs.client = nil
	}
	if cs.server != nil {
		cs.server.Stop()
		cs.server = nil
	}
}

func (cs *CandidateTestSuite) TearDownSuite() {
	cs.TearDownTest()
}

func (cs *CandidateTestSuite) config(conf *candidate.Config) *candidate.Config {
	if conf == nil {
		conf = new(candidate.Config)
	}
	conf.Client = cs.client
	return conf
}

func (cs *CandidateTestSuite) configKeyed(conf *candidate.Config) *candidate.Config {
	conf = cs.config(conf)
	conf.KVKey = testKVKey
	return conf
}

func (cs *CandidateTestSuite) makeCandidate(num int, conf *candidate.Config) *candidate.Candidate {
	lc := new(candidate.Config)
	if conf != nil {
		*lc = *conf
	}
	lc.ID = fmt.Sprintf("test-%d", num)
	if lc.SessionTTL == "" {
		lc.SessionTTL = lockTTL
	}
	cand, err := candidate.New(cs.configKeyed(lc))
	if err != nil {
		cs.T().Fatalf("err: %v", err)
	}

	return cand
}

func (cs *CandidateTestSuite) TestNew_EmptyKey() {
	_, err := candidate.New(cs.config(nil))
	require.NotNil(cs.T(), err, "Expected Empty Key error")
}

func (cs *CandidateTestSuite) TestNew_EmptyID() {
	cand, err := candidate.New(cs.configKeyed(nil))
	require.Nil(cs.T(), err, "Error creating candidate: %s", err)
	if myAddr, err := util.MyAddress(); err != nil {
		require.NotZero(cs.T(), cand.ID(), "Expected Candidate ID to not be empty")
	} else {
		require.Equal(cs.T(), myAddr, cand.ID(), "Expected Candidate ID to be \"%s\", saw \"%s\"", myAddr, cand.ID())
	}
}

func (cs *CandidateTestSuite) TestNew_InvalidID() {
	var err error

	_, err = candidate.New(cs.configKeyed(&candidate.Config{ID: "thursday dancing in the sun"}))
	require.Equal(cs.T(), candidate.InvalidCandidateID, err, "Expected \"thursday dancing in the sun\" to return invalid ID error, saw %+v", err)

	_, err = candidate.New(cs.configKeyed(&candidate.Config{ID: "Große"}))
	require.Equal(cs.T(), candidate.InvalidCandidateID, err, "Expected \"Große\" to return invalid ID error, saw %+v", err)
}

func (cs *CandidateTestSuite) TestRun_SimpleElectionCycle() {
	server, client := testutil.MakeServerAndClient(cs.T(), nil)
	cs.server = server
	cs.client = client.Client

	var candidate1, candidate2, candidate3, leaderCandidate *candidate.Candidate
	var leader *api.SessionEntry
	var err error

	wg := new(sync.WaitGroup)

	wg.Add(3)

	go func() {
		candidate1 = cs.makeCandidate(1, &candidate.Config{AutoRun: true})
		candidate1.Wait()
		wg.Done()
	}()
	go func() {
		candidate2 = cs.makeCandidate(2, &candidate.Config{AutoRun: true})
		candidate2.Wait()
		wg.Done()
	}()
	go func() {
		candidate3 = cs.makeCandidate(3, &candidate.Config{AutoRun: true})
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
	for i, cand := range []*candidate.Candidate{candidate1, candidate2, candidate3} {
		if leaderCandidate == cand {
			leadersFound = 1
			continue
		}

		require.True(
			cs.T(),
			0 == leadersFound || 1 == leadersFound,
			fmt.Sprintf("leaderCandidate matched to more than 1 candidate.  Iteration \"%d\"", i))

		require.False(
			cs.T(),
			cand.Elected(),
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

	// election re-enter attempt

	wg.Add(3)

	go func() {
		candidate1 = cs.makeCandidate(1, &candidate.Config{AutoRun: true})
		candidate1.Wait()
		wg.Done()
	}()
	go func() {
		candidate2 = cs.makeCandidate(2, &candidate.Config{AutoRun: true})
		candidate2.Wait()
		wg.Done()
	}()
	go func() {
		candidate3 = cs.makeCandidate(3, &candidate.Config{AutoRun: true})
		candidate3.Wait()
		wg.Done()
	}()

	wg.Wait()

	leader, err = candidate1.LeaderService()
	require.Nil(cs.T(), err, fmt.Sprintf("Unable to locate re-entered leader session entry: %v", err))

	// attempt to locate elected leader
	switch leader.ID {
	case candidate1.SessionID():
		leaderCandidate = candidate1
	case candidate2.SessionID():
		leaderCandidate = candidate2
	case candidate3.SessionID():
		leaderCandidate = candidate3
	default:
		leaderCandidate = nil
	}

	require.NotNil(
		cs.T(),
		leaderCandidate,
		fmt.Sprintf(
			"Expected one of \"%+v\", saw \"%s\"",
			[]string{candidate1.SessionID(), candidate2.SessionID(), candidate3.SessionID()},
			leader.ID))

	leadersFound = 0
	for i, cand := range []*candidate.Candidate{candidate1, candidate2, candidate3} {
		if leaderCandidate == cand {
			leadersFound = 1
			continue
		}

		require.True(
			cs.T(),
			0 == leadersFound || 1 == leadersFound,
			fmt.Sprintf("leaderCandidate matched to more than 1 candidate.  Iteration \"%d\"", i))

		require.False(
			cs.T(),
			cand.Elected(),
			fmt.Sprintf("Candidate \"%d\" is not elected but says that it is...", i))
	}
}

func (cs *CandidateTestSuite) TestRun_SessionAnarchy() {
	server, client := testutil.MakeServerAndClient(cs.T(), nil)
	cs.server = server
	cs.client = client.Client

	cand := cs.makeCandidate(1, &candidate.Config{AutoRun: true})

	updates := make([]candidate.ElectionUpdate, 0)
	updatesMu := sync.Mutex{}

	cand.Watch("", func(update candidate.ElectionUpdate) {
		updatesMu.Lock()
		cs.T().Logf("Update received: %#v", update)
		updates = append(updates, update)
		updatesMu.Unlock()
	})
	cand.Wait()

	sid := cand.SessionID()
	require.NotEmpty(cs.T(), sid, "Expected sid to contain value")

	cs.client.Session().Destroy(sid, nil)

	require.Equal(
		cs.T(),
		candidate.StateRunning,
		cand.State(),
		"Expected candidate state to still be %d after session destroyed",
		candidate.StateRunning)

	cand.Wait()

	require.NotEmpty(cs.T(), cand.SessionID(), "Expected new session id")
	require.NotEqual(cs.T(), sid, cand.SessionID(), "Expected new session id")

	updatesMu.Lock()
	require.Len(cs.T(), updates, 3, "Expected to see 3 updates")
	updatesMu.Unlock()
}
