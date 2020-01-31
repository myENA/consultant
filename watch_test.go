package consultant_test

import (
	"testing"

	"github.com/myENA/consultant/v2"
)

func TestWatch(t *testing.T) {
	t.Run("package-funcs", func(t *testing.T) {
		errs := make(map[string]error)
		_, errs["key"] = consultant.WatchKey("key", true, "", "")
		_, errs["keyprefix"] = consultant.WatchKeyPrefix("keyprefix", true, "", "")
		_, errs["nodes"] = consultant.WatchNodes(true, "", "")
		_, errs["services-stale"] = consultant.WatchServices(true, "", "")
		_, errs["service-tag-stale"] = consultant.WatchService("service", "tag", true, true, "", "")
		_, errs["checks-service"] = consultant.WatchChecks("service", "", true, "", "")
		_, errs["checks-state"] = consultant.WatchChecks("", "pass", true, "", "")
		_, errs["event"] = consultant.WatchEvent("event", "", "")
		_, errs["connect-roots"] = consultant.WatchConnectRoots("", "")
		_, errs["connect-leaf"] = consultant.WatchConnectLeaf("service", "", "")
		_, errs["agent-service"] = consultant.WatchAgentService("sid")

		for n, err := range errs {
			if err != nil {
				t.Logf("error creating plan %q: %s", n, err)
				t.Fail()
			}
		}
	})
}
