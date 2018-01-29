package candidate_test

import (
	"github.com/hashicorp/consul/api"
	cst "github.com/hashicorp/consul/testutil"
	"github.com/myENA/consultant"
	"github.com/myENA/consultant/candidate"
	"github.com/myENA/consultant/testutil"
	"github.com/myENA/consultant/util"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"testing"
)

const (
	testKVKey = "consultant/test/candidate-test"
	testID    = "test-candidate"
)

func init() {
	consultant.Debug()
}

type CandidateTestSuite struct {
	suite.Suite

	server *cst.TestServer
	client *api.Client

	candidate *candidate.Candidate
}

func TestCandidate(t *testing.T) {
	suite.Run(t, &CandidateTestSuite{})
}

func (cs *CandidateTestSuite) SetupSuite() {
	server, client := testutil.MakeServerAndClient(cs.T(), nil)
	cs.server = server
	cs.client = client.Client
}

func (cs *CandidateTestSuite) TearDownSuite() {
	if cs.candidate != nil {
		cs.candidate.Resign()
	}
	cs.candidate = nil
	if cs.server != nil {
		cs.server.Stop()
	}
	cs.server = nil
	cs.client = nil
}

func (cs *CandidateTestSuite) TearDownTest() {
	if cs.candidate != nil {
		cs.candidate.Resign()
	}
	cs.candidate = nil
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

func (cs *CandidateTestSuite) TestNew_EmptyKey() {
	_, err := candidate.New(cs.config(nil))
	require.NotNil(cs.T(), err, "Expected Empty Key error")
}

func (cs *CandidateTestSuite) TestNew_EmptyID() {
	var err error

	cs.candidate, err = candidate.New(cs.configKeyed(nil))
	require.Nil(cs.T(), err, "Error creating candidate: %s", err)

	if myAddr, err := util.MyAddress(); err != nil {
		require.NotZero(cs.T(), cs.candidate.ID(), "Expected Candidate ID to not be empty")
	} else {
		require.Equal(cs.T(), myAddr, cs.candidate.ID(), "Expected Candidate ID to be \"%s\", saw \"%s\"", myAddr, cs.candidate.ID())
	}
}

func (cs *CandidateTestSuite) TestNew_InvalidID() {
	var err error

	_, err = candidate.New(cs.configKeyed(&candidate.Config{ID: "thursday dancing in the sun"}))
	require.Equal(cs.T(), candidate.InvalidCandidateID, err, "Expected \"thursday dancing in the sun\" to return invalid ID error, saw %+v", err)

	_, err = candidate.New(cs.configKeyed(&candidate.Config{ID: "Große"}))
	require.Equal(cs.T(), candidate.InvalidCandidateID, err, "Expected \"Große\" to return invalid ID error, saw %+v", err)
}
