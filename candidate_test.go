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
	if cfg.ManagedSessionConfig.Logger == nil {
		cfg.ManagedSessionConfig.Logger = log.New(os.Stdout, "------> candidate-session ", log.LstdFlags)
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
	cfg.ManagedSessionConfig.Debug = true
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
	testRun := func(t *testing.T, cand *consultant.Candidate, testAsLeader bool) {
		if !cand.Running() {
			t.Log("Expected candidate to be running")
			t.Fail()
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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

		cand := newCandidateWithServerAndClient(t, nil, server, client)
		defer cand.Resign()

		if err := cand.Run(); err != nil {
			t.Logf("Error calling candidate.Run: %s", err)
			t.Fail()
			return
		}

		testRun(t, cand, true)
	})

	t.Run("single-auto-start", func(t *testing.T) {
		server, client := makeTestServerAndClient(t, nil)
		defer stopTestServer(server)
		server.WaitForSerfCheck(t)
		server.WaitForLeader(t)

		cfg := new(consultant.CandidateConfig)
		cfg.StartImmediately = true

		cand := newCandidateWithServerAndClient(t, cfg, server, client)
		defer cand.Resign()

		testRun(t, cand, true)
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

		makeCandidate := func(t *testing.T, cfg *consultant.CandidateConfig) *consultant.Candidate {
			cfg.StartImmediately = true
			return newCandidateWithServerAndClient(t, cfg, server, client)
		}

		t.Run("setup-candidates", func(t *testing.T) {
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

		t.Run("leader-defined", func(t *testing.T) {
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

		t.Run("state-sane", func(t *testing.T) {
			for _, cand := range cands {
				if leaderCandidate == *cand {
					testRun(t, *cand, true)
				} else {
					testRun(t, *cand, false)
				}
			}
		})

		if t.Failed() {
			t.Log("Candidate state not sane")
			return
		}

		wg.Add(3)

		t.Run("shutdown", func(t *testing.T) {
			for _, cand := range cands {
				go func(cand *consultant.Candidate) {
					defer wg.Done()
					if err := cand.Resign(); err != nil {
						t.Logf("Candidate %q - error during resignation: %s", cand.ID(), err)
					}
				}(*cand)
			}

			wg.Wait()

			for _, cand := range cands {
				if (*cand).Elected() {
					t.Logf("Candidate %q still thinks its elected", (*cand).ID())
					t.Fail()
				}
			}
		})

		if t.Failed() {
			t.Log("Candidates did not all Resign cleanly")
			return
		}

		wg.Add(3)

		t.Run("candidate-restart", func(t *testing.T) {
			for _, cand := range cands {
				go func(cand *consultant.Candidate) {
					defer wg.Done()
					if err := cand.Run(); err != nil {
						t.Logf("Error re-entering candidate %q into election pool: %s", cand.ID(), err)
						t.FailNow()
					} else if err := cand.Wait(); err != nil {
						t.Logf("Error waiting for re-election on candidate %q: %s", cand.ID(), err)
						t.FailNow()
					}
				}(*cand)
			}
		})

		wg.Wait()

		if t.Failed() {
			t.Log("Error re-entering candidates into election pool")
			return
		}
	})

	t.Run("session-anarchy", func(t *testing.T) {
		server, client := makeTestServerAndClient(t, nil)
		defer stopTestServer(server)
		server.WaitForSerfCheck(t)
		server.WaitForLeader(t)

		cfg := new(consultant.CandidateConfig)
		cfg.StartImmediately = true

		cand := newCandidateWithServerAndClient(t, cfg, server, client)
		defer cand.Resign()

		testRun(t, cand, true)

		sid := cand.Session().ID()

		if _, err := client.Session().Destroy(sid, nil); err != nil {
			t.Logf("Error destroying session: %s", err)
			t.Fail()
			return
		}

		if err := cand.Wait(); err != nil {
			t.Logf("Error waiting for re-election: %s", err)
			t.Fail()
			return
		}

		testRun(t, cand, true)
	})
}
