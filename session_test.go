package consultant_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
	cst "github.com/hashicorp/consul/sdk/testutil"
	"github.com/myENA/consultant/v2"
)

const (
	sessionTestName = "managed-session"
	sessionTestTTL  = "5s"
)

func newManagedSessionWithServerAndClient(t *testing.T, cb cst.ServerConfigCallback, cfg *consultant.ManagedSessionConfig) (*cst.TestServer, *consultant.Client, *consultant.ManagedSession) {
	server, client := makeTestServerAndClient(t, cb)
	server.WaitForSerfCheck(t)
	if cfg == nil {
		cfg = new(consultant.ManagedSessionConfig)
	}
	cfg.Client = client.Client
	cfg.Logger = log.New(os.Stdout, "", log.LstdFlags)
	cfg.Debug = true
	ms, err := consultant.NewManagedSession(cfg)
	if err != nil {
		_ = server.Stop()
		t.Fatalf("Error creating ManagedSession instance: %s", err)
	}
	return server, client, ms
}

func TestNewManagedSession(t *testing.T) {
	noConfigTests := map[string]struct {
		config *consultant.ManagedSessionConfig
	}{
		"config-nil": {
			config: nil,
		},
		"config-empty": {
			config: new(consultant.ManagedSessionConfig),
		},
	}

	for name, setup := range noConfigTests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ms, err := consultant.NewManagedSession(setup.config)
			if err != nil {
				t.Logf("expected no error, saw: %s", err)
				t.FailNow()
			}
			if t.Failed() {
				return
			}

			if ttl := ms.TTL(); ttl != consultant.SessionDefaultTTL {
				t.Logf("Expected TTL to be %q, saw %q:", consultant.SessionDefaultTTL, ttl)
				t.Fail()
			} else if renewInterval := ms.RenewInterval(); renewInterval != (ttl / 2) {
				t.Logf("Expected Renew Interval to be half of TTL (%d), saw %d", ttl/2, renewInterval)
				t.Fail()
			}
			if api.SessionBehaviorDelete != ms.TTLBehavior() {
				t.Logf("Expected TTLBehavior to be %q, saw %q", api.SessionBehaviorDelete, ms.TTLBehavior())
				t.Fail()
			}
			if lr := ms.LastRenewed(); !lr.IsZero() {
				t.Logf("Expected LastRenewed to be zero, saw %s", lr)
				t.Fail()
			}
		})
	}

	invalidFieldTests := map[string]struct {
		config *consultant.ManagedSessionConfig
		field  string
	}{
		"invalid-def-ttl": {
			config: &consultant.ManagedSessionConfig{
				Definition: &api.SessionEntry{
					TTL: "whatever",
				},
			},
			field: "Definition.TTL",
		},
		"invalid-behavior": {
			config: &consultant.ManagedSessionConfig{
				Definition: &api.SessionEntry{
					Behavior: "whatever",
				},
			},
			field: "Definition.Behavior",
		},
	}

	for name, setup := range invalidFieldTests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := consultant.NewManagedSession(setup.config)
			if err == nil {
				t.Logf("Expected error with invalid %q", setup.field)
				t.Fail()
			}
		})
	}

	ttlNormalizeTests := map[string]struct {
		ttl string
	}{
		"def-ttl-sub-minimum": {ttl: "1s"},
		"def-ttl-sup-maximum": {ttl: "72h"},
	}
	for name, setup := range ttlNormalizeTests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			def := api.SessionEntry{
				TTL: setup.ttl,
			}
			conf := consultant.ManagedSessionConfig{
				Definition: &def,
			}
			ms, err := consultant.NewManagedSession(&conf)
			if err != nil {
				t.Logf("Expected no error, saw %s", err)
				t.Fail()
			} else if mttl := ms.TTL(); mttl.String() == setup.ttl {
				t.Logf("Expected ttl to be normalized but saw original value: %s", mttl)
				t.Fail()
			} else if mttl < consultant.SessionMinimumTTL || mttl > consultant.SessionMaximumTTL {
				t.Logf("Expected ttl to be normalized to %s <= x >= %s, saw %s", consultant.SessionMinimumTTL, consultant.SessionMaximumTTL, mttl)
				t.Fail()
			}
		})
	}

	for _, b := range []string{"", api.SessionBehaviorDelete, api.SessionBehaviorRelease} {
		var tname string
		if b == "" {
			tname = "behavior-empty"
		} else {
			tname = fmt.Sprintf("behavior-%s", b)
		}
		t.Run(tname, func(t *testing.T) {
			t.Parallel()
			def := api.SessionEntry{
				Behavior: b,
			}
			conf := consultant.ManagedSessionConfig{
				Definition: &def,
			}
			ms, err := consultant.NewManagedSession(&conf)
			if err != nil {
				t.Logf("Expected no error, saw %s", err)
				t.Fail()
			} else if b == "" {
				if mb := ms.TTLBehavior(); mb != api.SessionBehaviorDelete {
					t.Logf("Expected default TTL behavior to be %q, saw %q", api.SessionBehaviorDelete, mb)
					t.Fail()
				}
			} else if mb := ms.TTLBehavior(); mb != b {
				t.Logf("Expected behavior to be %q, saw %q", b, mb)
				t.Fail()
			}
		})
	}
}

func TestManagedSession_Run(t *testing.T) {

	testRun := func(t *testing.T, ctx context.Context, ms *consultant.ManagedSession) {
		if !ms.Running() {
			t.Log("Expected session to be running")
			t.Fail()
			return
		}

		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		se, _, err := ms.SessionEntry(ctx)
		if err != nil {
			t.Logf("Error fetching session entry: %s", err)
			t.Fail()
			return
		}

		if se.ID != ms.ID() {
			t.Logf("Expected session id to be %q, saw %q", se.ID, ms.ID())
			t.Fail()
		}
		if lr := ms.LastRenewed(); lr.IsZero() {
			t.Log("Expected last renewed to be non-zero")
			t.Fail()
		}
	}

	t.Run("manual-start", func(t *testing.T) {
		server, _, ms := newManagedSessionWithServerAndClient(t, nil, nil)
		defer stopTestServer(server)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		if err := ms.Run(ctx); err != nil {
			t.Logf("Error running session: %s", err)
			t.Fail()
			return
		}
		testRun(t, ctx, ms)
	})

	t.Run("auto-start", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		cfg := new(consultant.ManagedSessionConfig)
		cfg.StartImmediately = ctx

		server, _, ms := newManagedSessionWithServerAndClient(t, nil, cfg)
		defer stopTestServer(server)
		testRun(t, ctx, ms)
	})

	t.Run("graceful-stop", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		cfg := new(consultant.ManagedSessionConfig)
		cfg.StartImmediately = ctx

		server, client, ms := newManagedSessionWithServerAndClient(t, nil, cfg)
		defer stopTestServer(server)
		testRun(t, ctx, ms)

		ctx, cancel = context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		sid := ms.ID()

		if err := ms.Stop(); err != nil {
			t.Logf("Error stopping session: %s", err)
			t.Fail()
			return
		}

		se, _, err := client.Session().Info(sid, nil)
		if err != nil {
			t.Logf("Error fetching session after stop: %s", err)
			t.Fail()
			return
		}

		if se != nil {
			t.Logf("Expected session to be nil, saw %v", se)
			t.Fail()
		}
	})
}

func TestManagedSession_PushStateNotification(t *testing.T) {
	var (
		running   int32
		created   int32
		destroyed int32
		stopped   int32
	)

	server, _, ms := newManagedSessionWithServerAndClient(t, nil, nil)
	defer stopTestServer(server)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ms.AttachNotificationHandler("", func(n consultant.Notification) {
		t.Logf("Incoming notification: %d (%[1]s)", n.Event)
		switch n.Event {
		case consultant.NotificationEventManagedSessionRunning:
			atomic.AddInt32(&running, 1)
		case consultant.NotificationEventManagedSessionCreate:
			atomic.AddInt32(&created, 1)
		case consultant.NotificationEventManagedSessionDestroy:
			atomic.AddInt32(&destroyed, 1)
		case consultant.NotificationEventManagedSessionStopped:
			atomic.AddInt32(&stopped, 1)

		default:
			t.Logf("Unexpected notification %d (%[1]s) seen", n.Event)
			t.Fail()
		}
	})

	if err := ms.Run(ctx); err != nil {
		t.Logf("Error running managed service: %s", err)
		t.Fail()
		return
	}
}

//func (ss *SessionTestSuite) TestSession_Run() {
//	var err error
//
//	upChan := make(chan consultant.ManagedSessionUpdate)
//	updateFunc := func(up consultant.ManagedSessionUpdate) {
//		upChan <- up
//	}
//
//	ss.session, err = consultant.NewManagedSession(ss.config(&consultant.ManagedSessionConfig{TTL: sessionTestTTL, UpdateFunc: updateFunc}))
//	require.Nil(ss.T(), err, "Error constructing session: %s", err)
//
//	ss.session.Run()
//
//	select {
//	case <-time.After(10 * time.Second):
//		ss.FailNow("We should have a session by now.")
//	case up := <-upChan:
//		require.NotZero(ss.T(), up.ID, "Expected ID to be populated: %+v", up)
//		require.NotZero(ss.T(), up.Name, "Expected Name to be populated: %+v", up)
//		require.NotZero(ss.T(), up.LastRenewed, "Expected LastRenewed to be non-zero: %+v", up)
//		require.Nil(ss.T(), up.Error, "Expected Error to be nil, saw: %s", up.Error)
//	}
//}
//
//func (ss *SessionTestSuite) TestSession_AutoRun() {
//	var err error
//
//	ss.session, err = consultant.NewManagedSession(ss.config(&consultant.ManagedSessionConfig{TTL: sessionTestTTL, StartImmediately: true}))
//	require.Nil(ss.T(), err, "Error constructing session: %s", err)
//
//	require.True(ss.T(), ss.session.Running(), "StartImmediately session not automatically started")
//}
//
//func (ss *SessionTestSuite) TestSession_SessionKilled() {
//	var (
//		initialID string
//		err       error
//	)
//
//	upChan := make(chan consultant.ManagedSessionUpdate, 1)
//	updateFunc := func(up consultant.ManagedSessionUpdate) {
//		upChan <- up
//	}
//
//	ss.session, err = consultant.NewManagedSession(ss.config(&consultant.ManagedSessionConfig{TTL: sessionTestTTL, UpdateFunc: updateFunc}))
//	require.Nil(ss.T(), err, "Error constructing session: %s", err)
//
//	ss.session.Run()
//
//TestLoop:
//	for i := 0; ; i++ {
//		select {
//		case up := <-upChan:
//			if i == 0 {
//				if up.ID == "" {
//					ss.FailNowf("Expected to have session on first pass", "ManagedSession create failed: %#v", up)
//					break TestLoop
//				}
//				initialID = up.ID
//				// take a nap...
//				time.Sleep(time.Second)
//				if _, err := ss.client.Session().Destroy(up.ID, nil); err != nil {
//					ss.FailNowf("Failed to arbitrarily destroy session", "Error: %s", err)
//					break TestLoop
//				}
//			} else if i == 1 {
//				if up.ID == "" {
//					ss.FailNowf("Expected to have new session on 2nd pass", "ManagedSession create failed: %#v", up)
//					break TestLoop
//				}
//				if up.ID == initialID {
//					ss.FailNowf("Expected different upstream session id", "Initial: %q; New: %q", initialID, up.ID)
//					break TestLoop
//				}
//				// if we got a new id, great!
//				break TestLoop
//			}
//		}
//	}
//}
//
//type SessionUtilTestSuite struct {
//	suite.Suite
//
//	server *cst.TestServer
//	client *api.Client
//
//	session *consultant.ManagedSession
//}
//
//func TestSessionUtil(t *testing.T) {
//	suite.Run(t, &SessionUtilTestSuite{})
//}
//
//func (us *SessionUtilTestSuite) SetupSuite() {
//	server, client := makeTestServerAndClient(us.T(), nil)
//	us.server = server
//	us.client = client.Client
//}
//
//func (us *SessionUtilTestSuite) TearDownSuite() {
//	if us.session != nil {
//		us.session.Stop()
//	}
//	us.session = nil
//	if us.server != nil {
//		us.server.Stop()
//	}
//	us.server = nil
//	us.client = nil
//}
//
//func (us *SessionUtilTestSuite) TearDownTest() {
//	if us.session != nil {
//		us.session.Stop()
//	}
//	us.session = nil
//}
//
//func (us *SessionUtilTestSuite) TestParseName_NoKey() {
//	var err error
//
//	updated := make(chan consultant.ManagedSessionUpdate, 1)
//	updater := func(up consultant.ManagedSessionUpdate) {
//		updated <- up
//	}
//
//	us.session, err = consultant.NewManagedSession(&consultant.ManagedSessionConfig{
//		Client:     us.client,
//		TTL:        "10s",
//		UpdateFunc: updater,
//	})
//	require.Nil(us.T(), err, "Error creating session: %s", err)
//
//	us.session.Run()
//
//	var up consultant.ManagedSessionUpdate
//
//	select {
//	case <-time.After(10 * time.Second):
//		us.FailNow("Expected to receive session update by now")
//	case up = <-updated:
//		require.Nil(us.T(), up.Error, "ManagedSession update error: %s", err)
//	}
//
//	name := us.session.Name()
//	require.NotZero(us.T(), name, "Expected name to be populated")
//
//	parsed, err := consultant.ParseManagedSessionName(name)
//	require.Nil(us.T(), err, "Error parsing session name: %s", err)
//
//	require.Zero(us.T(), parsed.Key, "Expected Key to be empty: %+v", parsed)
//	require.NotZero(us.T(), parsed.NodeName, "Expected NodeName to be populated: %+v", parsed)
//	require.NotZero(us.T(), parsed.RandomID, "Expected UseRandomID to be populated: %+v", parsed)
//
//	node, err := us.client.Agent().NodeName()
//	require.Nil(us.T(), err, "Unable to fetch node: %s", err)
//
//	require.Equal(us.T(), node, parsed.NodeName, "Expected NodeName to be \"%s\", saw \"%s\"", node, parsed.NodeName)
//}
//
//func (us *SessionUtilTestSuite) TestParseName_Keyed() {
//	var err error
//
//	updated := make(chan consultant.ManagedSessionUpdate, 1)
//	updater := func(up consultant.ManagedSessionUpdate) {
//		updated <- up
//	}
//
//	us.session, err = consultant.NewManagedSession(&consultant.ManagedSessionConfig{
//		Key:        clientTestKVKey,
//		Client:     us.client,
//		TTL:        "10s",
//		UpdateFunc: updater,
//	})
//	require.Nil(us.T(), err, "Error creating session: %s", err)
//
//	us.session.Run()
//
//	var up consultant.ManagedSessionUpdate
//
//	select {
//	case <-time.After(10 * time.Second):
//		us.FailNow("Expected to receive session update by now")
//	case up = <-updated:
//		require.Nil(us.T(), up.Error, "ManagedSession update error: %s", err)
//	}
//
//	name := us.session.Name()
//	require.NotZero(us.T(), name, "Expected name to be populated")
//
//	parsed, err := consultant.ParseManagedSessionName(name)
//	require.Nil(us.T(), err, "Error parsing session name: %s", err)
//
//	require.NotZero(us.T(), parsed.Key, "Expected Key to be populated: %+v", parsed)
//	require.NotZero(us.T(), parsed.NodeName, "Expected NodeName to be populated: %+v", parsed)
//	require.NotZero(us.T(), parsed.RandomID, "Expected UseRandomID to be populated: %+v", parsed)
//
//	node, err := us.client.Agent().NodeName()
//	require.Nil(us.T(), err, "Unable to fetch node: %s", err)
//
//	require.Equal(us.T(), sessionTestKey, parsed.Key, "Expected Key to equal \"%s\", but saw \"%s\"", sessionTestKey, parsed.Key)
//	require.Equal(us.T(), node, parsed.NodeName, "Expected NodeName to be \"%s\", saw \"%s\"", node, parsed.NodeName)
//}
