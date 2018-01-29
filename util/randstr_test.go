package util_test

import (
	"github.com/myENA/consultant/util"
	"github.com/stretchr/testify/suite"
	"testing"
)

type RandStrTestSuite struct {
	suite.Suite
}

func TestRandStr(t *testing.T) {
	suite.Run(t, new(RandStrTestSuite))
}

func (rs *RandStrTestSuite) checkAlphabet(in string) bool {
	var a, A, N bool

	for _, c := range in {
		if 'a' <= c && c <= 'z' {
			a = true
		} else if 'A' <= c && c <= 'Z' {
			A = true
		} else if '0' <= c && c <= '9' {
			N = true
		}
	}

	if !a {
		rs.T().Logf("RandStr \"%s\" does not contain [a-z]", in)
	}
	if !A {
		rs.T().Logf("RandStr \"%s\" does not contain [A-Z]", in)
	}
	if !N {
		rs.T().Logf("RandStr \"%s\" does not contain [0-9]", in)
	}

	return a && A && N
}

// TODO: doesn't really test anything, just do something.
func (rs *RandStrTestSuite) TestWorks() {
	var val string
	var ok bool

	values := make(map[string]struct{})

	for i := 1; i < 1000; i++ {
		val = util.RandStr(12)
		rs.checkAlphabet(val)
		if _, ok = values[val]; ok {
			rs.FailNow("Found duplicate after only \"%d\" iterations!", i)
		} else {
			values[val] = struct{}{}
		}
	}
}
