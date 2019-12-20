package consultant_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/consul/api"
	"github.com/myENA/consultant/v2"
)

const (
	sessionTestTTL = "5s"
)

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
		"invalid-ttl": {
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
		"sub-minimum": {ttl: "1s"},
		"sup-maximum": {ttl: "72h"},
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

//func (ss *SessionTestSuite) TestNew_Populated() {
//	var err error
//
//	ss.session, err = consultant.NewManagedSession(ss.config(&consultant.ManagedSessionConfig{TTL: "20s", TTLBehavior: api.SessionBehaviorDelete}))
//	require.Nil(ss.T(), err, "Error constructing with config: %s", err)
//
//	ttl := ss.session.TTL()
//	require.Equal(ss.T(), 20*time.Second, ttl, "Expected TTL of \"20s\", saw \"%s\"", ttl)
//
//	interval := ss.session.RenewInterval()
//	require.Equal(ss.T(), 10*time.Second, interval, "Expected Renew Interval of \"10s\", saw \"%s\"", interval)
//
//	behavior := ss.session.Behavior()
//	require.Equal(ss.T(), api.SessionBehaviorDelete, behavior, "Expected TTLBehavior \"%s\", saw \"%s\"", api.SessionBehaviorDelete, behavior)
//}
//
//func (ss *SessionTestSuite) TestNew_Failures() {
//	var err error
//
//	const badTTL = "thursday"
//	const badBehavior = "cheese place"
//
//	_, err = consultant.NewManagedSession(ss.config(&consultant.ManagedSessionConfig{TTL: badTTL}))
//	require.NotNil(ss.T(), err, "Expected TTL of \"%s\" to return error", badTTL)
//
//	_, err = consultant.NewManagedSession(ss.config(&consultant.ManagedSessionConfig{TTLBehavior: badBehavior}))
//	require.NotNil(ss.T(), err, "Expected TTLBehavior of \"%s\" to return error", badBehavior)
//}
//
//func (ss *SessionTestSuite) TestNew_TTLMinimum() {
//	var err error
//
//	ss.session, err = consultant.NewManagedSession(ss.config(&consultant.ManagedSessionConfig{TTL: "1s"}))
//	require.Nil(ss.T(), err, "Error constructing session: %s", err)
//
//	ttl := ss.session.TTL()
//	require.Equal(ss.T(), 10*time.Second, ttl, "Expected minimum allowable TTL to be \"10s\", saw \"%s\"", ttl)
//
//	interval := ss.session.RenewInterval()
//	require.Equal(ss.T(), 5*time.Second, interval, "Expected minimum RenewInterval to be \"5s\", saw \"%s\"", interval)
//}
//
//func (ss *SessionTestSuite) TestNew_TTLMaximum() {
//	var err error
//
//	ss.session, err = consultant.NewManagedSession(ss.config(&consultant.ManagedSessionConfig{TTL: "96400s"}))
//	require.Nil(ss.T(), err, "Error constructing session: %s", err)
//
//	ttl := ss.session.TTL()
//	require.Equal(ss.T(), 86400*time.Second, ttl, "Expected maximum allowable TTL to be \"86400s\", saw \"%s\"", ttl)
//
//	interval := ss.session.RenewInterval()
//	require.Equal(ss.T(), 43200*time.Second, interval, "Expected maximum allowable Renew Interval to be \"432000s\", saw \"%s\"", interval)
//}
//
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
