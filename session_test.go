package consultant_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
	cst "github.com/hashicorp/consul/sdk/testutil"
	"github.com/myENA/consultant/v2"
)

const (
	sessionTestName = "managed-session"
	sessionTestTTL  = "5s"
)

func newManagedSessionWithServerAndClient(t *testing.T, cfg *consultant.ManagedSessionConfig, server *cst.TestServer, client *consultant.Client) *consultant.ManagedSession {
	if cfg == nil {
		cfg = new(consultant.ManagedSessionConfig)
	}
	cfg.Client = client.Client
	cfg.Logger = log.New(os.Stdout, "---> managed-session ", log.LstdFlags)
	cfg.Debug = true
	ms, err := consultant.NewManagedSession(cfg)
	if err != nil {
		_ = server.Stop()
		t.Fatalf("Error creating ManagedSession instance: %s", err)
	}
	return ms
}

func buildManagedSessionServerAndClient(t *testing.T, cb cst.ServerConfigCallback, cfg *consultant.ManagedSessionConfig) (*cst.TestServer, *consultant.Client, *consultant.ManagedSession) {
	server, client := makeTestServerAndClient(t, cb)
	server.WaitForSerfCheck(t)
	return server, client, newManagedSessionWithServerAndClient(t, cfg, server, client)
}

func TestNewManagedSession(t *testing.T) {
	noConfigTests := map[string]struct {
		config *consultant.ManagedSessionConfig
	}{
		"config-nil": {
			config: nil,
		},
		"config-empty": {
			config: new(consultant.ManagedSessionConfig),
		},
	}

	for name, setup := range noConfigTests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ms, err := consultant.NewManagedSession(setup.config)
			if err != nil {
				t.Logf("expected no error, saw: %s", err)
				t.Fail()
			}
			if t.Failed() {
				_ = ms.Shutdown()
				return
			}

			if ttl := ms.TTL(); ttl != consultant.SessionDefaultTTL {
				t.Logf("Expected TTL to be %q, saw %q:", consultant.SessionDefaultTTL, ttl)
				t.Fail()
			} else if renewInterval := ms.RenewInterval(); renewInterval != (ttl / 2) {
				t.Logf("Expected Renew Interval to be half of TTL (%d), saw %d", ttl/2, renewInterval)
				t.Fail()
			}
			if api.SessionBehaviorDelete != ms.TTLBehavior() {
				t.Logf("Expected TTLBehavior to be %q, saw %q", api.SessionBehaviorDelete, ms.TTLBehavior())
				t.Fail()
			}
			if lr := ms.LastRenewed(); !lr.IsZero() {
				t.Logf("Expected LastRenewed to be zero, saw %s", lr)
				t.Fail()
			}

			_ = ms.Shutdown()
		})
	}

	invalidFieldTests := map[string]struct {
		config *consultant.ManagedSessionConfig
		field  string
	}{
		"invalid-def-ttl": {
			config: &consultant.ManagedSessionConfig{
				Definition: &api.SessionEntry{
					TTL: "whatever",
				},
			},
			field: "Definition.TTL",
		},
		"invalid-behavior": {
			config: &consultant.ManagedSessionConfig{
				Definition: &api.SessionEntry{
					Behavior: "whatever",
				},
			},
			field: "Definition.Behavior",
		},
	}

	for name, setup := range invalidFieldTests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ms, err := consultant.NewManagedSession(setup.config)
			if err == nil {
				t.Logf("Expected error with invalid %q", setup.field)
				t.Fail()
			}

			if ms != nil {
				_ = ms.Shutdown()
			}
		})
	}

	ttlNormalizeTests := map[string]struct {
		ttl string
	}{
		"def-ttl-sub-minimum": {ttl: "1s"},
		"def-ttl-sup-maximum": {ttl: "72h"},
	}
	for name, setup := range ttlNormalizeTests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			def := api.SessionEntry{
				TTL: setup.ttl,
			}
			conf := consultant.ManagedSessionConfig{
				Definition: &def,
			}
			ms, err := consultant.NewManagedSession(&conf)
			if err != nil {
				t.Logf("Expected no error, saw %s", err)
				t.Fail()
			} else if mttl := ms.TTL(); mttl.String() == setup.ttl {
				t.Logf("Expected ttl to be normalized but saw original value: %s", mttl)
				t.Fail()
			} else if mttl < consultant.SessionMinimumTTL || mttl > consultant.SessionMaximumTTL {
				t.Logf("Expected ttl to be normalized to %s <= x >= %s, saw %s", consultant.SessionMinimumTTL, consultant.SessionMaximumTTL, mttl)
				t.Fail()
			}

			if ms != nil {
				_ = ms.Shutdown()
			}
		})
	}

	for _, b := range []string{"", api.SessionBehaviorDelete, api.SessionBehaviorRelease} {
		var tname string
		if b == "" {
			tname = "behavior-empty"
		} else {
			tname = fmt.Sprintf("behavior-%s", b)
		}
		t.Run(tname, func(t *testing.T) {
			t.Parallel()
			def := api.SessionEntry{
				Behavior: b,
			}
			conf := consultant.ManagedSessionConfig{
				Definition: &def,
			}
			ms, err := consultant.NewManagedSession(&conf)
			if err != nil {
				t.Logf("Expected no error, saw %s", err)
				t.Fail()
			} else if b == "" {
				if mb := ms.TTLBehavior(); mb != api.SessionBehaviorDelete {
					t.Logf("Expected default TTL behavior to be %q, saw %q", api.SessionBehaviorDelete, mb)
					t.Fail()
				}
			} else if mb := ms.TTLBehavior(); mb != b {
				t.Logf("Expected behavior to be %q, saw %q", b, mb)
				t.Fail()
			}

			if ms != nil {
				_ = ms.Shutdown()
			}
		})
	}
}

func TestManagedSession_Run(t *testing.T) {

	testRun := func(t *testing.T, ms *consultant.ManagedSession) {
		if !ms.Running() {
			t.Log("Expected session to be running")
			t.Fail()
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		se, _, err := ms.SessionEntry(ctx)
		if err != nil {
			t.Logf("Error fetching session entry: %s", err)
			t.Fail()
		} else {
			if se.ID != ms.ID() {
				t.Logf("Expected session id to be %q, saw %q", se.ID, ms.ID())
				t.Fail()
			}
			if lr := ms.LastRenewed(); lr.IsZero() {
				t.Log("Expected last renewed to be non-zero")
				t.Fail()
			}
		}
	}

	t.Run("manual-start", func(t *testing.T) {
		server, _, ms := buildManagedSessionServerAndClient(t, nil, nil)
		defer stopTestServer(server)

		if err := ms.Run(); err != nil {
			t.Logf("Error running session: %s", err)
			t.Fail()
		} else {
			testRun(t, ms)
		}

		_ = ms.Shutdown()
	})

	t.Run("auto-start", func(t *testing.T) {
		cfg := new(consultant.ManagedSessionConfig)
		cfg.StartImmediately = true

		server, _, ms := buildManagedSessionServerAndClient(t, nil, cfg)
		defer stopTestServer(server)
		testRun(t, ms)

		_ = ms.Shutdown()
	})

	t.Run("graceful-stop", func(t *testing.T) {
		cfg := new(consultant.ManagedSessionConfig)
		cfg.StartImmediately = true

		server, client, ms := buildManagedSessionServerAndClient(t, nil, cfg)
		defer stopTestServer(server)
		testRun(t, ms)

		sid := ms.ID()

		if err := ms.Stop(); err != nil {
			t.Logf("Error stopping session: %s", err)
			t.Fail()
		} else if se, _, err := client.Session().Info(sid, nil); err != nil {
			t.Logf("Error fetching session after stop: %s", err)
			t.Fail()
		} else if se != nil {
			t.Logf("Expected session to be nil, saw %v", se)
			t.Fail()
		}

		_ = ms.Shutdown()
	})

	t.Run("persist-for-a-bit", func(t *testing.T) {
		var renewed int32
		se := new(api.SessionEntry)
		se.TTL = (consultant.SessionMinimumTTL).String()

		cfg := new(consultant.ManagedSessionConfig)
		cfg.Definition = se

		server, _, ms := buildManagedSessionServerAndClient(t, nil, cfg)
		defer stopTestServer(server)

		ms.AttachNotificationHandler("", func(n consultant.Notification) {
			if n.Event == consultant.NotificationEventManagedSessionRenew {
				atomic.AddInt32(&renewed, 1)
			}
		})

		if err := ms.Run(); err != nil {
			t.Logf("Error running session: %s", err)
			t.Fail()
		} else {

			<-time.After(3 * consultant.SessionMinimumTTL)

			if err := ms.Stop(); err != nil {
				t.Logf("Error during stop: %s", err)
				t.Fail()
			} else if renewed != 5 {
				t.Logf("Expected renewed to be 5, saw %d", renewed)
				t.Fail()
			}
		}

		_ = ms.Shutdown()
	})

	t.Run("recreate-after-kill", func(t *testing.T) {
		var (
			created   bool
			destroyed bool
			recreated bool
			initialID string

			updateChan = make(chan consultant.Notification, 10)
		)

		server, client, ms := buildManagedSessionServerAndClient(t, nil, nil)
		defer stopTestServer(server)

		ms.AttachNotificationChannel("", updateChan)

		defer func() {
			close(updateChan)
			if len(updateChan) > 0 {
				for range updateChan {
				}
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := ms.Run(); err != nil {
			t.Logf("Error running session: %s", err)
			t.Fail()

			_ = ms.Shutdown()

			return
		}

	TestLoop:
		for {
			select {
			case <-ctx.Done():
				t.Logf("Context ended: %s", ctx.Err())
				t.Fail()
				break TestLoop
			case n := <-updateChan:
				switch n.Event {
				case consultant.NotificationEventManagedSessionCreate:
					if ms.ID() == "" {
						t.Log("Expected ID() to return non-empty after create notification")
						t.Fail()
						break TestLoop
					}
					if !created {
						created = true
						_, err := client.Session().Destroy(ms.ID(), nil)
						if err != nil {
							t.Logf("Error manually destroying session %q: %s", ms.ID(), err)
							t.Fail()
							break TestLoop
						}
						destroyed = true
					} else if destroyed {
						if ms.ID() == initialID {
							t.Logf("Expected upstream session ID to be different after destroy -> create cycle, saw %q twice", ms.ID())
							t.Fail()
						} else {
							recreated = true
						}
						break TestLoop
					}
				}
			}
		}

		if !recreated {
			t.Log("Expected recreated to be true")
			t.Fail()
		}

		_ = ms.Shutdown()
	})
}

func TestManagedSession_PushStateNotification(t *testing.T) {
	var (
		running   int32
		created   int32
		destroyed int32
		stopped   int32
	)

	server, _, ms := buildManagedSessionServerAndClient(t, nil, nil)
	defer stopTestServer(server)

	ms.AttachNotificationHandler("", func(n consultant.Notification) {
		t.Logf("Incoming notification: %d (%[1]s)", n.Event)
		switch n.Event {
		case consultant.NotificationEventManagedSessionRunning:
			atomic.AddInt32(&running, 1)
		case consultant.NotificationEventManagedSessionCreate:
			atomic.AddInt32(&created, 1)
		case consultant.NotificationEventManagedSessionDestroy:
			atomic.AddInt32(&destroyed, 1)
		case consultant.NotificationEventManagedSessionStopped:
			atomic.AddInt32(&stopped, 1)

		default:
			t.Logf("Unexpected notification %d (%[1]s) seen", n.Event)
			t.Fail()
		}
	})

	if err := ms.Run(); err != nil {
		t.Logf("Error running managed service: %s", err)
		t.Fail()
	}

	_ = ms.Shutdown()
}
