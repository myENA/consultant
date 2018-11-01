package observe_test

import (
	"github.com/myENA/consultant/observe"
	"testing"
)

func TestWatchConstruction(t *testing.T) {
	t.Run("WatchKey", func(t *testing.T) {
		if _, err := observe.WatchKey("key", true, "", ""); err != nil {
			t.Logf("WatchKey err: %s", err)
			t.Fail()
		}
	})
	t.Run("WatchKeyPrefix", func(t *testing.T) {
		if _, err := observe.WatchKeyPrefix("keyprefix", true, "", ""); err != nil {
			t.Logf("WatchKeyPrefix err: %s", err)
			t.Fail()
		}
	})
	t.Run("WatchServices", func(t *testing.T) {
		if _, err := observe.WatchServices(true, "", ""); err != nil {
			t.Logf("WatchServices err: %s", err)
			t.Fail()
		}
	})
	t.Run("WatchNodes", func(t *testing.T) {
		if _, err := observe.WatchNodes(true, "", ""); err != nil {
			t.Logf("WatchNodes err: %s", err)
			t.Fail()
		}
	})
	t.Run("WatchService", func(t *testing.T) {
		if _, err := observe.WatchService("service", "tag", true, true, "", ""); err != nil {
			t.Logf("WatchService err: %s", err)
			t.Fail()
		}
	})
	t.Run("WatchChecks_Service", func(t *testing.T) {
		if _, err := observe.WatchChecks("service", "", true, "", ""); err != nil {
			t.Logf("WatchChecks_Service err: %s", err)
			t.Fail()
		}
	})
	t.Run("WatchChecks_State", func(t *testing.T) {
		if _, err := observe.WatchChecks("", "pass", true, "", ""); err != nil {
			t.Logf("WatchChecks_State err: %s", err)
			t.Fail()
		}
	})
	t.Run("WatchEvent", func(t *testing.T) {
		if _, err := observe.WatchEvent("event", "", ""); err != nil {
			t.Logf("WatchEvent err: %s", err)
			t.Fail()
		}
	})
	t.Run("WatchConnectRoots", func(t *testing.T) {
		if _, err := observe.WatchConnectRoots("", ""); err != nil {
			t.Logf("WatchConnectRoots err: %s", err)
			t.Fail()
		}
	})
	t.Run("WatchConnectLeaf", func(t *testing.T) {
		if _, err := observe.WatchConnectLeaf("service", "", ""); err != nil {
			t.Logf("WatchConnectLeaf err; %s", err)
			t.Fail()
		}
	})
	t.Run("WatchConnectProxyConfig", func(t *testing.T) {
		if _, err := observe.WatchProxyConfig("sid", "", ""); err != nil {
			t.Logf("WatchConnectProxyConfig err: %s", err)
			t.Fail()
		}
	})
}
