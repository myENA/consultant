package consultant_test

import (
	"fmt"
	"github.com/myENA/consultant"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"testing"
)

type WatchTestSuite struct {
	suite.Suite
}

func TestWatch(t *testing.T) {
	suite.Run(t, &WatchTestSuite{})
}

func (ws *WatchTestSuite) TestWatchConstruction() {
	var err error

	_, err = consultant.WatchKey("key", true, "", "")
	require.Nil(ws.T(), err, fmt.Sprintf("Unable to construct \"key\" watch plan: %s", err))

	_, err = consultant.WatchKeyPrefix("keyprefix", true, "", "")
	require.Nil(ws.T(), err, fmt.Sprintf("Unable to construct \"keyprefix\" watch plan: %s", err))

	_, err = consultant.WatchServices(true, "", "")
	require.Nil(ws.T(), err, fmt.Sprintf("Unable to construct \"services\" watch plan: %s", err))

	_, err = consultant.WatchNodes(true, "", "")
	require.Nil(ws.T(), err, fmt.Sprintf("Unable to construct \"nodes\" watch plan: %s", err))

	_, err = consultant.WatchService("service", "tag", true, true, "", "")
	require.Nil(ws.T(), err, fmt.Sprintf("Unable to construct \"service\" watch plan: %s", err))

	_, err = consultant.WatchChecks("service", "", true, "", "")
	require.Nil(ws.T(), err, fmt.Sprintf("Unable to construct \"checks\" watch plan for service: %s", err))
	_, err = consultant.WatchChecks("", "pass", true, "", "")
	require.Nil(ws.T(), err, fmt.Sprintf("Unable to construct \"checks\" watch plan for state: %s", err))

	_, err = consultant.WatchEvent("event", "", "")
	require.Nil(ws.T(), err, fmt.Sprintf("Unable to construct \"event\" watch plan: %s", err))
}
