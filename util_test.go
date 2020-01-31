package consultant_test

import (
	"math/rand"
	"testing"
	"time"

	"github.com/myENA/consultant/v2"
)

func TestLocalAddress(t *testing.T) {
	t.Run("ip", func(t *testing.T) {
		if _, err := consultant.LocalAddressIP(); err != nil {
			t.Logf("Error fetching RFC-1918 addr: %s", err)
			t.Fail()
		}
	})

	t.Run("str", func(t *testing.T) {

		if _, err := consultant.LocalAddress(); err != nil {
			t.Logf("Error matching RFC-1918 addr: %s", err)
			t.Fail()
		}
	})
}

func testRandStrCheckAlphabet(t *testing.T, in string) bool {
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
		t.Logf("LazyRandomString \"%s\" does not contain [a-z]", in)
	}
	if !A {
		t.Logf("LazyRandomString \"%s\" does not contain [A-Z]", in)
	}
	if !N {
		t.Logf("LazyRandomString \"%s\" does not contain [0-9]", in)
	}

	return a && A && N
}

func TestRandomString(t *testing.T) {
	t.Run("works", func(t *testing.T) {
		var (
			val string
			ok  bool
		)

		rand.Seed(time.Now().UnixNano())

		values := make(map[string]struct{})

		for i := 1; i < 1000; i++ {
			val = consultant.LazyRandomString(12)
			testRandStrCheckAlphabet(t, val)
			if _, ok = values[val]; ok {
				t.Logf("Found duplicate after only \"%d\" iterations!", i)
				t.FailNow()
			} else {
				values[val] = struct{}{}
			}
		}
	})
}
