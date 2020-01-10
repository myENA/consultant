package consultant_test

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
	cst "github.com/hashicorp/consul/sdk/testutil"
	"github.com/myENA/consultant/v2"
)

const (
	candidateTestKVKey = "consultant/test/candidate-test"
	candidateTestID    = "test-candidate"
	candidateLockTTL   = "5s"
)

func newCandidateWithServerAndClient(t *testing.T, cfg *consultant.CandidateConfig, server *cst.TestServer, client *consultant.Client) *consultant.Candidate {
	if cfg == nil {
		cfg = new(consultant.CandidateConfig)
	}
	if cfg.ManagedSessionConfig.Definition == nil {
		cfg.ManagedSessionConfig.Definition = new(api.SessionEntry)
	}
	if cfg.ManagedSessionConfig.Definition.TTL == "" {
		cfg.ManagedSessionConfig.Definition.TTL = consultant.SessionMinimumTTL.String()
	}
	if cfg.KVKey == "" {
		cfg.KVKey = candidateTestKVKey
	}
	if cfg.ID == "" {
		cfg.ID = candidateTestID
	}
	cfg.Client = client.Client
	cfg.Logger = log.New(os.Stdout, "---> candidate ", log.LstdFlags)
	cfg.Debug = true
	cand, err := consultant.NewCandidate(cfg)
	if err != nil {
		_ = server.Stop()
		t.Fatalf("Error creating Candidate instance: %s", err)
	}
	return cand
}

func TestNewCandidate(t *testing.T) {
	tests := map[string]struct {
		shouldErr bool
		config    *consultant.CandidateConfig
	}{
		"config-nil": {shouldErr: true},
		"kv-key-empty": {
			shouldErr: true,
			config:    new(consultant.CandidateConfig),
		},
		"invalid-session-ttl": {
			shouldErr: true,
			config: &consultant.CandidateConfig{
				ManagedSessionConfig: consultant.ManagedSessionConfig{
					Definition: &api.SessionEntry{TTL: "whatever"},
				},
			},
		},
		"valid": {
			config: &consultant.CandidateConfig{
				KVKey: candidateTestKVKey,
			},
		},
	}
	for name, setup := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := consultant.NewCandidate(setup.config)
			if setup.shouldErr {
				if err == nil {
					t.Log("Expected error, saw nil")
					t.Fail()
				}
			} else if err != nil {
				t.Logf("Unexpected error seen: %s", err)
				t.Fail()
			}
		})
	}
}

func TestCandidate_Run(t *testing.T) {
	testRun := func(t *testing.T, ctx context.Context, cand *consultant.Candidate, testAsLeader bool) {
		if !cand.Running() {
			t.Log("Expected candidate to be running")
			t.Fail()
			return
		}

		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if err := cand.WaitUntil(ctx); err != nil {
			t.Logf("Candidate election cycle took longer than expected to complete: %s", err)
			t.FailNow()
			return
		}

		ctx, cancel = context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		kv, _, err := cand.LeaderKV(ctx)
		if err != nil {
			t.Logf("Error fetching leader key: %s", err)
			t.FailNow()
			return
		}
		if kv.Value == nil {
			t.Log("kv.Value is nil")
			t.FailNow()
			return
		}
		kvValue := new(consultant.CandidateDefaultLeaderKVValue)
		if err := json.Unmarshal(kv.Value, kvValue); err != nil {
			t.Logf("Error unmarshalling kv.Value: %s", err)
			t.FailNow()
			return
		}

		if testAsLeader {
			if kvValue.LeaderID != cand.ID() {
				t.Logf("Expected elected leader KV to have LeaderID of %q, saw %v", cand.ID(), kvValue)
				t.FailNow()
				return
			}
		} else {
			if kvValue.LeaderID == cand.ID() {
				t.Logf("Expected leader KV to NOT have LeaderID of %q, saw (%v)", cand.ID(), kvValue)
				t.FailNow()
				return
			}
		}

		ctx, cancel = context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		se, _, err := cand.LeaderSession(ctx)
		if err != nil {
			t.Logf("Error fetching candidate session: %s", err)
			t.FailNow()
			return
		}

		if testAsLeader {
			if se.ID != cand.Session().ID() {
				t.Logf("Expected session returned from LeaderSession to be %q, saw %q", cand.Session().ID(), se.ID)
				t.FailNow()
				return
			}
		} else {
			if se.ID == cand.Session().ID() {
				t.Logf("Expected sesesion returned from LeaderSession to NOT be %q", cand.Session().ID())
				t.FailNow()
				return
			}
		}
	}

	t.Run("single-manual-start", func(t *testing.T) {
		server, client := makeTestServerAndClient(t, nil)
		defer stopTestServer(server)
		server.WaitForSerfCheck(t)
		server.WaitForLeader(t)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cand := newCandidateWithServerAndClient(t, nil, server, client)
		defer cand.Resign()

		if err := cand.Run(ctx); err != nil {
			t.Logf("Error calling candidate.Run: %s", err)
			t.Fail()
			return
		}

		testRun(t, ctx, cand, true)
	})

	t.Run("single-auto-start", func(t *testing.T) {
		server, client := makeTestServerAndClient(t, nil)
		defer stopTestServer(server)
		server.WaitForSerfCheck(t)
		server.WaitForLeader(t)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cfg := new(consultant.CandidateConfig)
		cfg.StartImmediately = ctx

		cand := newCandidateWithServerAndClient(t, cfg, server, client)
		defer cand.Resign()

		testRun(t, ctx, cand, true)
	})

	t.Run("typical", func(t *testing.T) {
		var (
			server                                              *cst.TestServer
			client                                              *consultant.Client
			candidate1, candidate2, candidate3, leaderCandidate *consultant.Candidate
			leaderSession                                       *api.SessionEntry
			err                                                 error

			cands = []**consultant.Candidate{&candidate1, &candidate2, &candidate3}
			wg    = new(sync.WaitGroup)
		)

		wg.Add(3)

		server, client = makeTestServerAndClient(t, nil)
		server.WaitForSerfCheck(t)
		server.WaitForLeader(t)

		runCTX, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		makeCandidate := func(t *testing.T, cfg *consultant.CandidateConfig) *consultant.Candidate {
			cfg.StartImmediately = runCTX
			return newCandidateWithServerAndClient(t, cfg, server, client)
		}

		t.Run("typical-setup-candidates", func(t *testing.T) {
			go func() {
				defer wg.Done()
				candidate1 = makeCandidate(t, &consultant.CandidateConfig{ID: "test-1"})
				if err := candidate1.Wait(); err != nil {
					t.Logf("error waiting on candidate 1: %s", err)
					t.FailNow()
				}
			}()
			go func() {
				defer wg.Done()
				candidate2 = makeCandidate(t, &consultant.CandidateConfig{ID: "test-2"})
				if err := candidate2.Wait(); err != nil {
					t.Logf("error waiting on candidate 2: %s", err)
					t.FailNow()
				}
			}()
			go func() {
				defer wg.Done()
				candidate3 = makeCandidate(t, &consultant.CandidateConfig{ID: "test-3"})
				if err := candidate3.Wait(); err != nil {
					t.Logf("error waiting on candidate 3: %s", err)
					t.FailNow()
				}
			}()
		})

		wg.Wait()

		if t.Failed() {
			t.Logf("candidate setup failed")
			return
		}

		t.Run("typical-leader-defined", func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			leaderSession, _, err = candidate1.LeaderSession(ctx)
			if err != nil {
				t.Logf("Error fetching leader sesesion: %s", err)
				t.FailNow()
				return
			}

			switch leaderSession.ID {
			case candidate1.Session().ID():
				leaderCandidate = candidate1
			case candidate2.Session().ID():
				leaderCandidate = candidate2
			case candidate3.Session().ID():
				leaderCandidate = candidate3
			}

			if leaderCandidate == nil {
				t.Logf(
					"None of the constructed candidates (%v) is the leader: %v",
					[]string{candidate1.Session().ID(), candidate2.Session().ID(), candidate3.Session().ID()},
					leaderSession,
				)
				t.FailNow()
			}
		})

		for _, cand := range cands {
			if leaderCandidate == *cand {
				testRun(t, runCTX, *cand, true)
			} else {
				testRun(t, runCTX, *cand, false)
			}

		}

		if t.Failed() {
			return
		}

		wg.Add(3)

		for _, cand := range cands {
			go func(cand *consultant.Candidate) {
				defer wg.Done()
				cand.Resign()
			}(*cand)
		}

		wg.Wait()

		for _, cand := range cands {
			if (*cand).Elected() {
				t.Logf("Candidate %q still thinks its elected", (*cand).ID())
				t.Fail()
			}
		}

		if t.Failed() {
			return
		}

	})
}

//
//func (cs *CandidateTestSuite) config(conf *consultant.CandidateConfig) *consultant.CandidateConfig {
//	if conf == nil {
//		conf = new(consultant.CandidateConfig)
//	}
//	conf.Client = cs.client
//	return conf
//}
//
//func (cs *CandidateTestSuite) configKeyed(conf *consultant.CandidateConfig) *consultant.CandidateConfig {
//	conf = cs.config(conf)
//	conf.KVKey = candidateTestKVKey
//	return conf
//}
//
//func (cs *CandidateTestSuite) makeCandidate(num int, conf *consultant.CandidateConfig) *consultant.Candidate {
//	lc := new(consultant.CandidateConfig)
//	if conf != nil {
//		*lc = *conf
//	}
//	lc.ID = fmt.Sprintf("test-%d", num)
//	if lc.SessionTTL == "" {
//		lc.SessionTTL = candidateLockTTL
//	}
//	cand, err := consultant.NewCandidate(cs.configKeyed(lc))
//	if err != nil {
//		cs.T().Fatalf("err: %v", err)
//	}
//
//	return cand
//}
//
//func (cs *CandidateTestSuite) TestRun_SimpleElectionCycle() {
//	server, client := makeTestServerAndClient(cs.T(), nil)
//	cs.server = server
//	cs.client = client.Client
//
//	var candidate1, candidate2, candidate3, leaderCandidate *consultant.Candidate
//	var leader *api.SessionEntry
//	var err error
//
//	wg := new(sync.WaitGroup)
//
//	wg.Add(3)
//
//	go func() {
//		candidate1 = cs.makeCandidate(1, &consultant.CandidateConfig{AutoRun: true})
//		candidate1.Wait()
//		wg.Done()
//	}()
//	go func() {
//		candidate2 = cs.makeCandidate(2, &consultant.CandidateConfig{AutoRun: true})
//		candidate2.Wait()
//		wg.Done()
//	}()
//	go func() {
//		candidate3 = cs.makeCandidate(3, &consultant.CandidateConfig{AutoRun: true})
//		candidate3.Wait()
//		wg.Done()
//	}()
//
//	wg.Wait()
//
//	leader, err = candidate1.LeaderSession()
//	require.Nil(cs.T(), err, fmt.Sprintf("Unable to locate leader session entry: %v", err))
//
//	// attempt to locate elected leader
//	switch leader.ID {
//	case candidate1.SessionID():
//		leaderCandidate = candidate1
//	case candidate2.SessionID():
//		leaderCandidate = candidate2
//	case candidate3.SessionID():
//		leaderCandidate = candidate3
//	}
//
//	require.NotNil(
//		cs.T(),
//		leaderCandidate,
//		fmt.Sprintf(
//			"Expected one of \"%+v\", saw \"%s\"",
//			[]string{candidate1.SessionID(), candidate2.SessionID(), candidate3.SessionID()},
//			leader.ID))
//
//	leadersFound := 0
//	for i, cand := range []*consultant.Candidate{candidate1, candidate2, candidate3} {
//		if leaderCandidate == cand {
//			leadersFound = 1
//			continue
//		}
//
//		require.True(
//			cs.T(),
//			0 == leadersFound || 1 == leadersFound,
//			fmt.Sprintf("leaderCandidate matched to more than 1 Candidate  Iteration \"%d\"", i))
//
//		require.False(
//			cs.T(),
//			cand.Elected(),
//			fmt.Sprintf("Candidate \"%d\" is not elected but says that it is...", i))
//	}
//
//	wg.Add(3)
//
//	go func() {
//		candidate1.Resign()
//		wg.Done()
//	}()
//	go func() {
//		candidate2.Resign()
//		wg.Done()
//	}()
//	go func() {
//		candidate3.Resign()
//		wg.Done()
//	}()
//
//	wg.Wait()
//
//	leader, err = candidate1.LeaderSession()
//	require.NotNil(cs.T(), err, "Expected empty key error, got nil")
//	require.Nil(cs.T(), leader, fmt.Sprintf("Expected nil leader, got %v", leader))
//
//	// election re-enter attempt
//
//	wg.Add(3)
//
//	go func() {
//		candidate1 = cs.makeCandidate(1, &consultant.CandidateConfig{AutoRun: true})
//		candidate1.Wait()
//		wg.Done()
//	}()
//	go func() {
//		candidate2 = cs.makeCandidate(2, &consultant.CandidateConfig{AutoRun: true})
//		candidate2.Wait()
//		wg.Done()
//	}()
//	go func() {
//		candidate3 = cs.makeCandidate(3, &consultant.CandidateConfig{AutoRun: true})
//		candidate3.Wait()
//		wg.Done()
//	}()
//
//	wg.Wait()
//
//	leader, err = candidate1.LeaderSession()
//	require.Nil(cs.T(), err, fmt.Sprintf("Unable to locate re-entered leader session entry: %v", err))
//
//	// attempt to locate elected leader
//	switch leader.ID {
//	case candidate1.SessionID():
//		leaderCandidate = candidate1
//	case candidate2.SessionID():
//		leaderCandidate = candidate2
//	case candidate3.SessionID():
//		leaderCandidate = candidate3
//	default:
//		leaderCandidate = nil
//	}
//
//	require.NotNil(
//		cs.T(),
//		leaderCandidate,
//		fmt.Sprintf(
//			"Expected one of \"%+v\", saw \"%s\"",
//			[]string{candidate1.SessionID(), candidate2.SessionID(), candidate3.SessionID()},
//			leader.ID))
//
//	leadersFound = 0
//	for i, cand := range []*consultant.Candidate{candidate1, candidate2, candidate3} {
//		if leaderCandidate == cand {
//			leadersFound = 1
//			continue
//		}
//
//		require.True(
//			cs.T(),
//			0 == leadersFound || 1 == leadersFound,
//			fmt.Sprintf("leaderCandidate matched to more than 1 Candidate  Iteration \"%d\"", i))
//
//		require.False(
//			cs.T(),
//			cand.Elected(),
//			fmt.Sprintf("Candidate \"%d\" is not elected but says that it is...", i))
//	}
//}
//
//func (cs *CandidateTestSuite) TestRun_SessionAnarchy() {
//	server, client := makeTestServerAndClient(cs.T(), nil)
//	cs.server = server
//	cs.client = client.Client
//
//	cand := cs.makeCandidate(1, &consultant.CandidateConfig{AutoRun: true})
//
//	updates := make([]consultant.CandidateUpdate, 0)
//	updatesMu := sync.Mutex{}
//
//	cand.Watch("", func(update consultant.CandidateUpdate) {
//		updatesMu.Lock()
//		cs.T().Logf("Update received: %#v", update)
//		updates = append(updates, update)
//		updatesMu.Unlock()
//	})
//	cand.Wait()
//
//	sid := cand.SessionID()
//	require.NotEmpty(cs.T(), sid, "Expected sid to contain value")
//
//	cs.client.Session().Destroy(sid, nil)
//
//	require.Equal(
//		cs.T(),
//		consultant.CandidateStateRunning,
//		cand.State(),
//		"Expected candidate state to still be %d after session destroyed",
//		consultant.CandidateStateRunning)
//
//	cand.Wait()
//
//	require.NotEmpty(cs.T(), cand.SessionID(), "Expected new session id")
//	require.NotEqual(cs.T(), sid, cand.SessionID(), "Expected new session id")
//
//	updatesMu.Lock()
//	require.Len(cs.T(), updates, 3, "Expected to see 3 updates")
//	updatesMu.Unlock()
//}
//
//type CandidateUtilTestSuite struct {
//	suite.Suite
//
//	server *cst.TestServer
//	client *api.Client
//
//	candidate *consultant.Candidate
//}
//
//func TestCandidate_Util(t *testing.T) {
//	suite.Run(t, &CandidateUtilTestSuite{})
//}
//
//func (us *CandidateUtilTestSuite) SetupSuite() {
//	server, client := makeTestServerAndClient(us.T(), nil)
//	us.server = server
//	us.client = client.Client
//}
//
//func (us *CandidateUtilTestSuite) TearDownSuite() {
//	if us.candidate != nil {
//		us.candidate.Resign()
//	}
//	us.candidate = nil
//	if us.server != nil {
//		us.server.Stop()
//	}
//	us.server = nil
//	us.client = nil
//}
//
//func (us *CandidateUtilTestSuite) TearDownTest() {
//	if us.candidate != nil {
//		us.candidate.Resign()
//	}
//	us.candidate = nil
//}
//
//func (us *CandidateUtilTestSuite) config(conf *consultant.CandidateConfig) *consultant.CandidateConfig {
//	if conf == nil {
//		conf = new(consultant.CandidateConfig)
//	}
//	conf.Client = us.client
//	return conf
//}
//
//func (us *CandidateUtilTestSuite) TestSessionNameParse() {
//	var err error
//
//	myAddr, err := consultant.LocalAddress()
//	if err != nil {
//		us.T().Skipf("Skipping TestSessionNameParse as local addr is indeterminate: %s", err)
//		us.T().SkipNow()
//		return
//	}
//
//	us.candidate, err = consultant.NewCandidate(us.config(&consultant.CandidateConfig{KVKey: candidateTestKVKey, SessionTTL: "10s"}))
//	require.Nil(us.T(), err, "Error creating candidate: %s", err)
//
//	us.candidate.Run()
//
//	err = us.candidate.WaitUntil(20 * time.Second)
//	require.Nil(us.T(), err, "Wait deadline of 20s breached: %s", err)
//
//	ip, err := us.candidate.LeaderIP()
//	require.Nil(us.T(), err, "Error locating Leader Service: %s", err)
//
//	require.Equal(us.T(), myAddr, ip.String(), "Expected Leader IP to be \"%s\", saw \"%s\"", myAddr, ip)
//}
