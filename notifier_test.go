package consultant_test

import (
	"log"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
			ms := consultant.NewBasicNotifier(log.New(os.Stdout, "==> Notifier ", log.LstdFlags), true)
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
		nt := consultant.NewBasicNotifier(log.New(os.Stdout, "==> Notifier ", log.LstdFlags), true)
		if ok := nt.DetachNotificationRecipient("whatever"); ok {
			t.Log("Expected false, saw true")
			t.Fail()
		}
	})
}

func TestNotifierBase_DetachAllNotificationRecipients(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		nt := consultant.NewBasicNotifier(log.New(os.Stdout, "==> Notifier ", log.LstdFlags), true)
		if cnt := nt.DetachAllNotificationRecipients(true); cnt != 0 {
			t.Logf("Expected 0, saw %d", cnt)
			t.Fail()
		}
	})
}

func TestBasicNotifier(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		t.Parallel()

		var (
			ti        int
			wg        *sync.WaitGroup
			respMu    sync.Mutex
			responses map[string]interface{}
			tests     map[string]struct {
				fn consultant.NotificationHandler
				ch chan consultant.Notification
			}

			bn = consultant.NewBasicNotifier(log.New(os.Stdout, "==> Notifier ", log.LstdFlags), true)
		)

		defer bn.DetachAllNotificationRecipients(false)

		wg = new(sync.WaitGroup)
		wg.Add(4)

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
			var id string
			if setup.ch != nil {
				id, _ = bn.AttachNotificationChannel("", setup.ch)
				go func(name string, ch chan consultant.Notification, id string) {
					n := <-ch
					respMu.Lock()
					responses[name] = n.Data
					respMu.Unlock()
					wg.Done()
					bn.DetachNotificationRecipient(id)
				}(name, setup.ch, id)
			} else if setup.fn != nil {
				id, _ = bn.AttachNotificationHandler("", setup.fn)
			} else {
				t.Fatalf("Test %q has no ch or fn defined", name)
				return
			}
			t.Logf("%s attached with id %q", name, id)
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
	})

	t.Run("blocking", func(t *testing.T) {
		t.Parallel()

		var (
			fn1cnt uint64
			fn2cnt uint64
			fn3cnt uint64

			fn1 consultant.NotificationHandler
			fn2 consultant.NotificationHandler
			fn3 consultant.NotificationHandler

			wg = new(sync.WaitGroup)
			bn = consultant.NewBasicNotifier(log.New(os.Stdout, "==> Notifier ", log.LstdFlags), true)
		)

		wg.Add(3)

		fn1 = func(n consultant.Notification) {
			atomic.AddUint64(&fn1cnt, 1)
			wg.Done()
		}
		fn2 = func(n consultant.Notification) {
			atomic.AddUint64(&fn2cnt, 1)
			time.Sleep(time.Second)
		}
		fn3 = func(n consultant.Notification) {
			atomic.AddUint64(&fn3cnt, 1)
			time.Sleep(5 * time.Second)
		}

		bn.AttachNotificationHandlers(fn1, fn2, fn3)

		go func() {
			for i := 0; i < 3; i++ {
				bn.Push(consultant.NotificationSourceTest, consultant.NotificationEventTestPush, i)
			}
		}()

		wg.Wait()

		final1, final2, final3 := atomic.LoadUint64(&fn1cnt), atomic.LoadUint64(&fn2cnt), atomic.LoadUint64(&fn3cnt)

		if final1 != 3 {
			t.Logf("Expected first handler to be called 3 times, saw %d", final1)
			t.Fail()
		}

		if final1 <= final2 || final1 <= final3 {
			t.Logf("Expected first handler to be called more times than 2nd or 3rd, saw: %d %d %d", final1, final2, final3)
			t.Fail()
		}

		bn.DetachAllNotificationRecipients(true)
	})
}
