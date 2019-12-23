package consultant_test

import (
	"sync"
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

func TestNewBasicNotifier(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		t.Parallel()

		var (
			ti        int
			wg        *sync.WaitGroup
			ids       [4]string
			respMu    sync.Mutex
			responses map[string]interface{}
			tests     map[string]struct {
				fn consultant.NotificationHandler
				ch chan consultant.Notification
			}

			bn = consultant.NewBasicNotifier()
		)

		wg = new(sync.WaitGroup)
		wg.Add(4)

		ids = [4]string{}
		responses = make(map[string]interface{}, 4)

		tests = map[string]struct {
			fn consultant.NotificationHandler
			ch chan consultant.Notification
		}{
			"ch1": {ch: make(chan consultant.Notification, 1)},
			"ch2": {ch: make(chan consultant.Notification, 1)},
			"fn1": {
				fn: func(n consultant.Notification) {
					respMu.Lock()
					responses["fn1"] = n.Data
					respMu.Unlock()
					wg.Done()
				},
			},
			"fn2": {
				fn: func(n consultant.Notification) {
					respMu.Lock()
					responses["fn2"] = n.Data
					respMu.Unlock()
					wg.Done()
				},
			},
		}

		defer close(tests["ch1"].ch)
		defer close(tests["ch2"].ch)

		for name, setup := range tests {
			if setup.ch != nil {
				ids[ti], _ = bn.AttachNotificationChannel("", setup.ch)
				go func(name string, ch chan consultant.Notification) {
					n := <-ch
					respMu.Lock()
					responses[name] = n.Data
					respMu.Unlock()
					wg.Done()
				}(name, setup.ch)
			} else if setup.fn != nil {
				ids[ti], _ = bn.AttachNotificationHandler("", setup.fn)
			} else {
				t.Fatalf("Test %q has no ch or fn defined", name)
				return
			}
			t.Logf("%s attached with id %q", name, ids[ti])
			ti++
		}

		bn.Push(consultant.NotificationSourceTest, consultant.NotificationEventTestPush, "hello there")

		wg.Wait()

		for name, resp := range responses {
			if s, ok := resp.(string); !ok {
				t.Logf("Response from %q expected to be string, saw %T", name, resp)
				t.Fail()
			} else if s != "hello there" {
				t.Logf("Expected response from %q to be %q, saw %q", name, "hello there", s)
				t.Fail()
			}
		}

		for _, id := range ids {
			if !bn.DetachNotificationRecipient(id) {
				t.Logf("Expected return to be true when removing recipient %q but saw false", id)
				t.FailNow()
			}
		}
	})
}
