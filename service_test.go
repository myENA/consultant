package consultant_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/consul/api"
	"github.com/myENA/consultant/v2"
)

const (
	managedServiceName = "managed"
	managedServicePort = 1423

	managedServiceAddedTag = "sandwiches!"
)

func TestNewManagedServiceBuilder(t *testing.T) {
	localAddr := getTestLocalAddr(t)

	tests := map[string]struct {
		base     *api.AgentServiceRegistration
		mutators []consultant.MangedAgentServiceRegistrationMutator
	}{
		"base-no-mutators": {
			base: &api.AgentServiceRegistration{
				Name:    managedServiceName,
				Port:    managedServicePort,
				Address: localAddr,
			},
		},
		"base-mutators": {
			base: &api.AgentServiceRegistration{
				Address: localAddr,
			},
			mutators: []consultant.MangedAgentServiceRegistrationMutator{
				func(builder *consultant.ManagedAgentServiceRegistration) {
					builder.Name = managedServiceName
					builder.Port = managedServicePort
				},
			},
		},
		"nil-base-mutators": {
			mutators: []consultant.MangedAgentServiceRegistrationMutator{
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

func TestNewBareManagedServiceBuilder(t *testing.T) {
	localAddr := getTestLocalAddr(t)

	tests := map[string]struct {
		name     string
		port     int
		mutators []consultant.MangedAgentServiceRegistrationMutator
	}{
		"args-no-mutators": {
			name: managedServiceName,
			port: managedServicePort,
		},
		"no-args-mutators": {
			mutators: []consultant.MangedAgentServiceRegistrationMutator{func(builder *consultant.ManagedAgentServiceRegistration) {
				builder.Name = managedServiceName
				builder.Port = managedServicePort
			}},
		},
		"args-mutators": {
			port: managedServicePort,
			mutators: []consultant.MangedAgentServiceRegistrationMutator{func(builder *consultant.ManagedAgentServiceRegistration) {
				builder.Name = managedServiceName
			}},
		},
		"args-mutators-override": {
			name: "something",
			port: 90001,
			mutators: []consultant.MangedAgentServiceRegistrationMutator{func(builder *consultant.ManagedAgentServiceRegistration) {
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

func TestManagedServiceBuilder_SetID(t *testing.T) {
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

func TestManagedServiceBuilder_Build(t *testing.T) {
	var (
		localAddr = getTestLocalAddr(t)
	)

	server, client := makeTestServerAndClient(t, nil)
	defer func() {
		// TODO: may not be sufficient...
		_ = server.Stop()
	}()

	server.WaitForSerfCheck(t)

	cfg := new(consultant.ManagedServiceConfig)
	cfg.Debug = true
	cfg.Logger = log.New(os.Stdout, "", log.LstdFlags)
	cfg.Client = client.Client

	b := consultant.NewBareManagedAgentServiceRegistration(managedServiceName, managedServicePort)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ms, err := b.Create(ctx, cfg)

	if err != nil {
		t.Logf("Error calling .Create(): %s", err)
		t.FailNow()
	} else if ms == nil {
		t.Log("No error, but ManagedService is nil")
		t.FailNow()
	}

	if t.Failed() {
		return
	}

	t.Log(client.Catalog().Service(managedServiceName, "", nil))

	svc, _, err := client.Agent().Service(b.ID, nil)
	if err != nil {
		t.Logf("Error fetching new service from consul: %s", err)
		t.FailNow()
	}

	if t.Failed() {
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
}
