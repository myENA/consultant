package consultant_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
	cst "github.com/hashicorp/consul/sdk/testutil"
	"github.com/myENA/consultant/v2"
)

const (
	managedServiceName = "managed"
	managedServicePort = 1423

	managedServiceAddedTag = "sandwiches!"
)

func newManagedServiceWithServerAndClient(
	t *testing.T,
	svcReg *consultant.ManagedAgentServiceRegistration,
	cfg *consultant.ManagedServiceConfig,
	server *cst.TestServer,
	client *consultant.Client) *consultant.ManagedService {

	var (
		ms  *consultant.ManagedService
		err error
	)

	if cfg == nil {
		cfg = new(consultant.ManagedServiceConfig)
	}

	cfg.Client = client.Client
	cfg.Logger = log.New(os.Stdout, "---> managed service ", log.LstdFlags)
	cfg.Debug = true

	if svcReg == nil {
		svcReg = consultant.NewBareManagedAgentServiceRegistration(managedServiceName, managedServicePort)
	}
	if svcReg.Address == "" {
		svcReg.Address = getTestLocalAddr(t)
	}

	svcReg.EnableTagOverride = true

	if ms, err = svcReg.Create(cfg); err != nil {
		_ = server.Stop()
		t.Fatalf("Error creating service: %s", err)
	}

	return ms
}

func TestNewManagedAgentServiceRegistration(t *testing.T) {
	localAddr := getTestLocalAddr(t)

	tests := map[string]struct {
		base     *api.AgentServiceRegistration
		mutators []consultant.ManagedAgentServiceRegistrationMutator
	}{
		"def-no-mutators": {
			base: &api.AgentServiceRegistration{
				Name:    managedServiceName,
				Port:    managedServicePort,
				Address: localAddr,
			},
		},
		"def-mutators": {
			base: &api.AgentServiceRegistration{
				Address: localAddr,
			},
			mutators: []consultant.ManagedAgentServiceRegistrationMutator{
				func(builder *consultant.ManagedAgentServiceRegistration) {
					builder.Name = managedServiceName
					builder.Port = managedServicePort
				},
			},
		},
		"nil-def-mutators": {
			mutators: []consultant.ManagedAgentServiceRegistrationMutator{
				func(builder *consultant.ManagedAgentServiceRegistration) {
					builder.Address = localAddr
				},
				func(builder *consultant.ManagedAgentServiceRegistration) {
					builder.Name = managedServiceName
				},
				func(builder *consultant.ManagedAgentServiceRegistration) {
					builder.Port = managedServicePort
				},
			},
		},
	}

	for name, setup := range tests {
		t.Run(name, func(t *testing.T) {
			b := consultant.NewManagedAgentServiceRegistration(setup.base, setup.mutators...)
			if b.Name != managedServiceName {
				t.Logf("Expected Name %q, saw %q", managedServiceName, b.Name)
				t.Fail()
			}
			if b.Port != managedServicePort {
				t.Logf("Expected Port %d, saw %d", managedServicePort, b.Port)
				t.Fail()
			}
			if b.Address != localAddr {
				t.Logf("Expected Address %q, saw %q", localAddr, b.Address)
				t.Fail()
			}
			if b.Kind != "" {
				t.Logf("Expected Kind to be empty, saw %q", b.Kind)
				t.Fail()
			}
		})
	}
}

func TestNewBareManagedAgentServiceRegistration(t *testing.T) {
	localAddr := getTestLocalAddr(t)

	tests := map[string]struct {
		name     string
		port     int
		mutators []consultant.ManagedAgentServiceRegistrationMutator
	}{
		"args-no-mutators": {
			name: managedServiceName,
			port: managedServicePort,
		},
		"no-args-mutators": {
			mutators: []consultant.ManagedAgentServiceRegistrationMutator{func(builder *consultant.ManagedAgentServiceRegistration) {
				builder.Name = managedServiceName
				builder.Port = managedServicePort
			}},
		},
		"args-mutators": {
			port: managedServicePort,
			mutators: []consultant.ManagedAgentServiceRegistrationMutator{func(builder *consultant.ManagedAgentServiceRegistration) {
				builder.Name = managedServiceName
			}},
		},
		"args-mutators-override": {
			name: "something",
			port: 90001,
			mutators: []consultant.ManagedAgentServiceRegistrationMutator{func(builder *consultant.ManagedAgentServiceRegistration) {
				builder.Name = managedServiceName
				builder.Port = managedServicePort
			}},
		},
	}
	for name, setup := range tests {
		t.Run(name, func(t *testing.T) {
			b := consultant.NewBareManagedAgentServiceRegistration(setup.name, setup.port, setup.mutators...)
			if b.Name != managedServiceName {
				t.Logf("Expected Name %q, saw %q", managedServiceName, b.Name)
				t.Fail()
			}
			if b.Port != managedServicePort {
				t.Logf("Expected Port %d, saw %d", managedServicePort, b.Port)
				t.Fail()
			}
			if b.Address != localAddr {
				t.Logf("Expected Address %q, saw %q", localAddr, b.Address)
				t.Fail()
			}
			if b.Kind != "" {
				t.Logf("Expected Kind to be empty, saw %q", b.Kind)
				t.Fail()
			}
		})
	}
}

func TestManagedAgentServiceRegistration_SetID(t *testing.T) {
	localAddr := getTestLocalAddr(t)

	tests := map[string]struct {
		format   string
		args     []interface{}
		expected *regexp.Regexp
	}{
		"no-anchors": {
			format:   "really-good-id",
			expected: regexp.MustCompile("^really-good-id$"),
		},
		"addr-anchor": {
			format:   "test-!ADDR!",
			expected: regexp.MustCompile(fmt.Sprintf("^test-%s$", localAddr)),
		},
		"name-anchor": {
			format:   "test-!NAME!",
			expected: regexp.MustCompile(fmt.Sprintf("^test-%s$", managedServiceName)),
		},
		"rand-anchor": {
			format:   "test-!RAND!",
			expected: regexp.MustCompile("^test-[a-zA-Z0-9]{12}$"),
		},
		"mixed-anchors": {
			format:   "test-!NAME!-!ADDR!-!RAND!",
			expected: regexp.MustCompile(fmt.Sprintf("^test-%s-%s-[a-zA-Z0-9]{12}$", managedServiceName, localAddr)),
		},
		"anchor-format": {
			format:   "test-%s",
			args:     []interface{}{"!ADDR!"},
			expected: regexp.MustCompile(fmt.Sprintf("^test-%s$", localAddr)),
		},
	}

	for name, setup := range tests {
		t.Run(name, func(t *testing.T) {
			b := consultant.NewBareManagedAgentServiceRegistration(managedServiceName, managedServicePort)
			b.SetID(setup.format, setup.args...)
			if !setup.expected.MatchString(b.ID) {
				t.Logf("Expected Name to match %q, saw %q", setup.expected, b.ID)
				t.Fail()
			}
		})
	}
}

func TestManagedAgentServiceRegistration_Create(t *testing.T) {
	var (
		localAddr = getTestLocalAddr(t)
	)

	server, client := makeTestServerAndClient(t, nil)
	defer stopTestServer(server)
	server.WaitForSerfCheck(t)

	cfg := new(consultant.ManagedServiceConfig)
	cfg.Debug = true
	cfg.Logger = log.New(os.Stdout, "", log.LstdFlags)
	cfg.Client = client.Client

	b := consultant.NewBareManagedAgentServiceRegistration(managedServiceName, managedServicePort)
	ms, err := b.Create(cfg)

	if err != nil {
		t.Logf("Error calling .Create(): %s", err)
		t.Fail()
	} else if ms == nil {
		t.Log("No error, but ManagedService is nil")
		t.Fail()
	}

	if t.Failed() {
		_ = ms.Shutdown()
		return
	}

	t.Log(client.Catalog().Service(managedServiceName, "", nil))

	svc, _, err := client.Agent().Service(b.ID, nil)
	if err != nil {
		t.Logf("Error fetching new service from consul: %s", err)
		t.Fail()
	}

	if t.Failed() {
		_ = ms.Shutdown()
		return
	}

	if svc.ID != b.ID {
		t.Logf("Expected id %q, saw %q", b.ID, svc.ID)
		t.Fail()
	}
	if svc.Service != b.Name {
		t.Logf("Expected name %q, saw %q", b.Name, svc.Service)
		t.Fail()
	}
	if svc.Address != localAddr {
		t.Logf("Expected address %q, saw %q", b.Address, svc.Address)
		t.Fail()
	}

	_ = ms.Shutdown()
}

func TestManagedService(t *testing.T) {
	server, client := makeTestServerAndClient(t, nil)
	defer stopTestServer(server)
	server.WaitForSerfCheck(t)

	ms := newManagedServiceWithServerAndClient(t, nil, nil, server, client)

	t.Run("add-tags", func(t *testing.T) {
		if added, err := ms.AddTags("new1", "new2"); err != nil {
			t.Logf("Error adding 2 tags: %s", err)
			t.Fail()
		} else if added != 2 {
			t.Logf("Expected added to be 2, saw %d", added)
			t.Fail()
		}
	})

	if t.Failed() {
		_ = ms.Shutdown()
		return
	}

	t.Run("remove-tags", func(t *testing.T) {
		if removed, err := ms.RemoveTags("new1", "new2"); err != nil {
			t.Logf("Error removing 2 tags: %s", err)
			t.Fail()
		} else if removed != 2 {
			t.Logf("Expected removed to be 2, saw %d", removed)
			t.Fail()
		}
	})

	if t.Failed() {
		_ = ms.Shutdown()
		return
	}

	t.Run("re-register", func(t *testing.T) {
		var (
			err error

			done  = make(chan struct{})
			notes = make(consultant.NotificationChannel, 100)
		)

		ms.AttachNotificationChannel("", notes)
		defer func() {
			close(notes)
			if len(notes) > 0 {
				for range notes {
				}
			}
		}()

		go func() {
			var missing uint64

			timer := time.NewTimer(30 * time.Second)

			defer close(done)

			for {
				select {
				case note := <-notes:
					t.Logf("notification received: %v", note)
					switch note.Event {
					case consultant.NotificationEventManagedServiceMissing:
						atomic.AddUint64(&missing, 1)

					case consultant.NotificationEventManagedServiceRefreshed:
						if 0 < atomic.LoadUint64(&missing) {
							d := note.Data.(consultant.ManagedServiceUpdate)
							if d.Error != nil {
								t.Logf("Received error in refresh update after missing: %s", err)
								t.Fail()
							} else {
								ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
								defer cancel()
								if svc, _, err := ms.AgentService(ctx); err != nil {
									t.Logf("Error retreiving after missing -> re-register loop: %s", err)
									t.Fail()
								} else if svc == nil {
									t.Log("Service not found after missing -> re-register loop")
									t.Fail()
								}
							}
							return
						}
					}
				case <-timer.C:
					t.Log("Service did not refresh within expected interval")
					t.Fail()
					return
				}
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		svc, _, err := ms.AgentService(ctx)
		if err != nil {
			t.Logf("Error calling .AgentService(): %s", err)
			t.Fail()
		}

		if err := client.Agent().ServiceDeregister(svc.ID); err != nil {
			t.Logf("Error deregistering service: %s", err)
			t.Fail()
		}

		// wait around for done to happen...
		<-done

		_ = ms.Shutdown()
	})
}
