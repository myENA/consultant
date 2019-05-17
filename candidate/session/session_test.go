package session_test

import (
	"github.com/hashicorp/consul/api"
	cst "github.com/hashicorp/consul/sdk/testutil"
	"github.com/myENA/consultant"
	"github.com/myENA/consultant/candidate/session"
	"github.com/myENA/consultant/testutil"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"testing"
	"time"
)

const (
	testKey = "session-test"
	testTTL = "5s"
)

func init() {
	consultant.Debug()
}

type SessionTestSuite struct {
	suite.Suite

	server *cst.TestServer
	client *api.Client

	session *session.Session
}

func TestSession(t *testing.T) {
	suite.Run(t, &SessionTestSuite{})
}

func (ss *SessionTestSuite) SetupSuite() {
	server, client := testutil.MakeServerAndClient(ss.T(), nil)
	ss.server = server
	ss.client = client.Client
}

func (ss *SessionTestSuite) TearDownSuite() {
	if ss.session != nil {
		ss.session.Stop()
	}
	ss.session = nil
	if ss.server != nil {
		ss.server.Stop()
	}
	ss.client = nil
	ss.server = nil
}

func (ss *SessionTestSuite) TearDownTest() {
	if ss.session != nil {
		ss.session.Stop()
	}
	ss.session = nil
}

func (ss *SessionTestSuite) config(conf *session.Config) *session.Config {
	if conf == nil {
		conf = new(session.Config)
	}
	conf.Client = ss.client
	return conf
}

func (ss *SessionTestSuite) TestNew_Empty() {
	var err error

	ss.session, err = session.New(ss.config(nil))
	require.Nil(ss.T(), err, "Error constructing empty: %s", err)

	ttl := ss.session.TTL()
	require.Equal(ss.T(), 30*time.Second, ttl, "Expected default TTL of 30s, saw \"%s\"", ttl)

	interval := ss.session.RenewInterval()
	require.Equal(ss.T(), 15*time.Second, interval, "Expected default Renew Interval of 15s, saw \"%s\"", interval)

	behavior := ss.session.Behavior()
	require.Equal(ss.T(), api.SessionBehaviorRelease, behavior, "Expected default Behavior to be \"%s\", saw \"%s\"", api.SessionBehaviorRelease, behavior)
}

func (ss *SessionTestSuite) TestNew_Populated() {
	var err error

	ss.session, err = session.New(ss.config(&session.Config{TTL: "20s", Behavior: api.SessionBehaviorDelete}))
	require.Nil(ss.T(), err, "Error constructing with config: %s", err)

	ttl := ss.session.TTL()
	require.Equal(ss.T(), 20*time.Second, ttl, "Expected TTL of \"20s\", saw \"%s\"", ttl)

	interval := ss.session.RenewInterval()
	require.Equal(ss.T(), 10*time.Second, interval, "Expected Renew Interval of \"10s\", saw \"%s\"", interval)

	behavior := ss.session.Behavior()
	require.Equal(ss.T(), api.SessionBehaviorDelete, behavior, "Expected Behavior \"%s\", saw \"%s\"", api.SessionBehaviorDelete, behavior)
}

func (ss *SessionTestSuite) TestNew_Failures() {
	var err error

	const badTTL = "thursday"
	const badBehavior = "cheese place"

	_, err = session.New(ss.config(&session.Config{TTL: badTTL}))
	require.NotNil(ss.T(), err, "Expected TTL of \"%s\" to return error", badTTL)

	_, err = session.New(ss.config(&session.Config{Behavior: badBehavior}))
	require.NotNil(ss.T(), err, "Expected Behavior of \"%s\" to return error", badBehavior)
}

func (ss *SessionTestSuite) TestNew_TTLMinimum() {
	var err error

	ss.session, err = session.New(ss.config(&session.Config{TTL: "1s"}))
	require.Nil(ss.T(), err, "Error constructing session: %s", err)

	ttl := ss.session.TTL()
	require.Equal(ss.T(), 10*time.Second, ttl, "Expected minimum allowable TTL to be \"10s\", saw \"%s\"", ttl)

	interval := ss.session.RenewInterval()
	require.Equal(ss.T(), 5*time.Second, interval, "Expected minimum RenewInterval to be \"5s\", saw \"%s\"", interval)
}

func (ss *SessionTestSuite) TestNew_TTLMaximum() {
	var err error

	ss.session, err = session.New(ss.config(&session.Config{TTL: "96400s"}))
	require.Nil(ss.T(), err, "Error constructing session: %s", err)

	ttl := ss.session.TTL()
	require.Equal(ss.T(), 86400*time.Second, ttl, "Expected maximum allowable TTL to be \"86400s\", saw \"%s\"", ttl)

	interval := ss.session.RenewInterval()
	require.Equal(ss.T(), 43200*time.Second, interval, "Expected maximum allowable Renew Interval to be \"432000s\", saw \"%s\"", interval)
}

func (ss *SessionTestSuite) TestSession_Run() {
	var err error

	upChan := make(chan session.Update)
	updateFunc := func(up session.Update) {
		upChan <- up
	}

	ss.session, err = session.New(ss.config(&session.Config{TTL: testTTL, UpdateFunc: updateFunc}))
	require.Nil(ss.T(), err, "Error constructing session: %s", err)

	ss.session.Run()

	select {
	case <-time.After(10 * time.Second):
		ss.FailNow("We should have a session by now.")
	case up := <-upChan:
		require.NotZero(ss.T(), up.ID, "Expected ID to be populated: %+v", up)
		require.NotZero(ss.T(), up.Name, "Expected Name to be populated: %+v", up)
		require.NotZero(ss.T(), up.LastRenewed, "Expected LastRenewed to be non-zero: %+v", up)
		require.Nil(ss.T(), up.Error, "Expected Error to be nil, saw: %s", up.Error)
	}
}

func (ss *SessionTestSuite) TestSession_AutoRun() {
	var err error

	ss.session, err = session.New(ss.config(&session.Config{TTL: testTTL, AutoRun: true}))
	require.Nil(ss.T(), err, "Error constructing session: %s", err)

	require.True(ss.T(), ss.session.Running(), "AutoRun session not automatically started")
}

func (ss *SessionTestSuite) TestSession_SessionKilled() {
	var (
		initialID string
		err       error
	)

	upChan := make(chan session.Update, 1)
	updateFunc := func(up session.Update) {
		upChan <- up
	}

	ss.session, err = session.New(ss.config(&session.Config{TTL: testTTL, UpdateFunc: updateFunc}))
	require.Nil(ss.T(), err, "Error constructing session: %s", err)

	ss.session.Run()

TestLoop:
	for i := 0; ; i++ {
		select {
		case up := <-upChan:
			if i == 0 {
				if up.ID == "" {
					ss.FailNowf("Expected to have session on first pass", "Session create failed: %#v", up)
					break TestLoop
				}
				initialID = up.ID
				// take a nap...
				time.Sleep(time.Second)
				if _, err := ss.client.Session().Destroy(up.ID, nil); err != nil {
					ss.FailNowf("Failed to arbitrarily destroy session", "Error: %s", err)
					break TestLoop
				}
			} else if i == 1 {
				if up.ID == "" {
					ss.FailNowf("Expected to have new session on 2nd pass", "Session create failed: %#v", up)
					break TestLoop
				}
				if up.ID == initialID {
					ss.FailNowf("Expected different upstream session id", "Initial: %q; New: %q", initialID, up.ID)
					break TestLoop
				}
				// if we got a new id, great!
				break TestLoop
			}
		}
	}
}
