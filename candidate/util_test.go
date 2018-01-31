package candidate_test

import (
	"github.com/hashicorp/consul/api"
	cst "github.com/hashicorp/consul/testutil"
	"github.com/myENA/consultant/candidate"
	"github.com/myENA/consultant/testutil"
	"github.com/myENA/consultant/util"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"testing"
	"time"
)

type UtilTestSuite struct {
	suite.Suite

	server *cst.TestServer
	client *api.Client

	candidate *candidate.Candidate
}

func TestCandidate_Util(t *testing.T) {
	suite.Run(t, &UtilTestSuite{})
}

func (us *UtilTestSuite) SetupSuite() {
	server, client := testutil.MakeServerAndClient(us.T(), nil)
	us.server = server
	us.client = client.Client
}

func (us *UtilTestSuite) TearDownSuite() {
	if us.candidate != nil {
		us.candidate.Resign()
	}
	us.candidate = nil
	if us.server != nil {
		us.server.Stop()
	}
	us.server = nil
	us.client = nil
}

func (us *UtilTestSuite) TearDownTest() {
	if us.candidate != nil {
		us.candidate.Resign()
	}
	us.candidate = nil
}

func (us *UtilTestSuite) config(conf *candidate.Config) *candidate.Config {
	if conf == nil {
		conf = new(candidate.Config)
	}
	conf.Client = us.client
	return conf
}

func (us *UtilTestSuite) TestSessionNameParse() {
	var err error

	myAddr, err := util.MyAddress()
	if err != nil {
		us.T().Skipf("Skipping TestSessionNameParse as local addr is indeterminate: %s", err)
		us.T().SkipNow()
		return
	}

	us.candidate, err = candidate.New(us.config(&candidate.Config{KVKey: testKVKey, SessionTTL: "10s"}))
	require.Nil(us.T(), err, "Error creating candidate: %s", err)

	us.candidate.Run()

	err = us.candidate.WaitFor(20 * time.Second)
	require.Nil(us.T(), err, "Wait deadline of 20s breached: %s", err)

	ip, err := us.candidate.LeaderIP()
	require.Nil(us.T(), err, "Error locating Leader Service: %s", err)

	require.Equal(us.T(), myAddr, ip.String(), "Expected Leader IP to be \"%s\", saw \"%s\"", myAddr, ip)
}
