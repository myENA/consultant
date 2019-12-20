package consultant_test

import (
	"testing"

	"github.com/myENA/consultant/v2"
)

func TestNotifierBase_AttachNotificationHandler(t *testing.T) {
	attachHandlerTests := map[string]struct {
		id string
		fn consultant.NotificationHandler
	}{
		"no-name": {
			fn: func(_ consultant.Notification) {},
		},
		"no-fn": {
			id: "panic!",
		},
		"valid": {
			id: "hellothere",
			fn: func(_ consultant.Notification) {},
		},
	}
	for name, setup := range attachHandlerTests {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if v := recover(); v != nil && setup.fn != nil {
					t.Logf("Unexpected panic when fn was not nil: %v", v)
					t.Fail()
				}
			}()
			ms := consultant.NewBasicNotifier()
			id, replaced := ms.AttachNotificationHandler(setup.id, setup.fn)
			if setup.id == "" {
				if id == "" {
					t.Log("Expected random ID to be created, saw empty string")
					t.Fail()
				}
			} else if setup.id != id {
				t.Logf("Expected ID to be %q, saw %q", setup.id, id)
				t.Fail()
			}
			if replaced {
				t.Log("Expected replaced to be false, saw true")
				t.Fail()
			}
		})
	}
}
func TestNotifierBase_DetachNotificationRecipient(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		nt := consultant.NewBasicNotifier()
		if ok := nt.DetachNotificationRecipient("whatever"); ok {
			t.Log("Expected false, saw true")
			t.Fail()
		}
	})
}

func TestNotifierBase_DetachAllNotificationRecipients(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		nt := consultant.NewBasicNotifier()
		if cnt := nt.DetachAllNotificationRecipients(); cnt != 0 {
			t.Logf("Expected 0, saw %d", cnt)
			t.Fail()
		}
	})

}
