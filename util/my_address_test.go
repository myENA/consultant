package util_test

import (
	"github.com/myENA/consultant/util"
	"github.com/stretchr/testify/suite"
	"testing"
)

type MyAddressTestSuite struct {
	suite.Suite
}

func TestMyAddress(t *testing.T) {
	suite.Run(t, new(MyAddressTestSuite))
}

func (ms *MyAddressTestSuite) TestMyAddress() {
	addr, err := util.MyAddress()
	if err != nil {
		ms.T().Logf("Unable to find interface with RFC1918 addr, this may or may not be a problem: %s", err)
	} else {
		ms.T().Logf("Found RFC1918 addr: %s", addr)
	}
}
