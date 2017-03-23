package consultant_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/testutil"
	"github.com/myENA/consultant"
)

func makeCandidate(t *testing.T, num int, client *api.Client) *consultant.Candidate {
	candidate, err := consultant.NewCandidate(client, fmt.Sprintf("test-%d", num), "consultant/tests/candidate-lock", "5s")
	if nil != err {
		t.Fatalf("err: %v", err)
	}

	return candidate
}

func TestSimpleElectionCycle(t *testing.T) {
	t.Parallel()

	var client *api.Client
	var server *testutil.TestServer
	var candidate1, candidate2, candidate3 *consultant.Candidate
	var leader *api.SessionEntry
	var err error

	client, server = makeClient(t)
	defer server.Stop()

	wg := new(sync.WaitGroup)

	wg.Add(3)

	go func() {
		candidate1 = makeCandidate(t, 1, client)
		candidate1.Wait()
		wg.Done()
	}()
	go func() {
		candidate2 = makeCandidate(t, 2, client)
		candidate2.Wait()
		wg.Done()
	}()
	go func() {
		candidate3 = makeCandidate(t, 3, client)
		candidate3.Wait()
		wg.Done()
	}()

	wg.Wait()

	t.Run("locate leader", func(t *testing.T) {
		leader, err = candidate1.Leader()
		if nil != err {
			t.Logf("Unable to locate leader session entry: %v", err)
			t.FailNow()
		}
	})

	t.Run("verify session match", func(t *testing.T) {
		if leader.ID != candidate1.SessionID() && leader.ID != candidate2.SessionID() && leader.ID != candidate3.SessionID() {
			t.Logf(
				"Expected one of \"%v\", saw \"%s\"",
				[]string{candidate1.SessionID(), candidate2.SessionID(), candidate3.SessionID()},
				leader.ID)
			t.FailNow()
		}
	})

	t.Run("resign leadership", func(t *testing.T) {
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
		if nil == err {
			t.Log("Expected empty key error, got nil")
			t.FailNow()
		}

		if nil != leader {
			t.Logf("Expected nil leader, got %v", leader)
			t.FailNow()
		}
	})
}
