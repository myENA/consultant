package consultant_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/testutil"
	"github.com/myENA/consultant"
)

func init() {
	consultant.Debug()
}

func makeClient(t *testing.T) (*api.Client, *testutil.TestServer) {
	conf := api.DefaultConfig()

	server := testutil.NewTestServerConfig(t, nil)
	conf.Address = server.HTTPAddr

	client, err := api.NewClient(conf)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	return client, server
}

func makeCandidate(t *testing.T, num int, client *api.Client) *consultant.Candidate {
	candidate, err := consultant.New(fmt.Sprintf("test-%d", num), "candidate/tests/lock", "5s", client)
	if nil != err {
		t.Fatalf("err: %v", err)
	}

	return candidate
}

func TestSimpleElectionCycle(t *testing.T) {
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
			t.Logf("Unable to locate leader session entry: %s", err.Error())
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
		wg = new(sync.WaitGroup)
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
