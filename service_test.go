package consultant_test

import (
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
	localAddr, err := consultant.LocalAddress()
	if err != nil {
		t.Logf("error finding local addr: %s", err)
		t.FailNow()
	}

	tests := map[string]struct {
		base     *api.AgentServiceRegistration
		mutators []consultant.ManagedServiceBuilderMutator
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
			mutators: []consultant.ManagedServiceBuilderMutator{
				func(builder *consultant.ManagedServiceBuilder) {
					builder.Name = managedServiceName
					builder.Port = managedServicePort
				},
			},
		},
		"nil-base-mutators": {
			mutators: []consultant.ManagedServiceBuilderMutator{
				func(builder *consultant.ManagedServiceBuilder) {
					builder.Address = localAddr
				},
				func(builder *consultant.ManagedServiceBuilder) {
					builder.Name = managedServiceName
				},
				func(builder *consultant.ManagedServiceBuilder) {
					builder.Port = managedServicePort
				},
			},
		},
	}

	for name, setup := range tests {
		t.Run(name, func(t *testing.T) {
			b := consultant.NewManagedServiceBuilder(setup.base, setup.mutators...)
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
			if b.Tags == nil {
				t.Log("Expected Tags to be non-nil")
				t.Fail()
			}
			if b.Checks == nil {
				t.Log("Expected Checks to be non-nil")
				t.Fail()
			}
			if b.TaggedAddresses == nil {
				t.Log("Expected TaggedAddresses to be non-nil")
				t.Fail()
			}
			if b.Meta == nil {
				t.Log("Expected Meta to be non-nil")
				t.Fail()
			}
		})
	}
}

func TestNewBareManagedServiceBuilder(t *testing.T) {
	localAddr, err := consultant.LocalAddress()
	if err != nil {
		t.Logf("error finding local addr: %s", err)
		t.FailNow()
	}

	tests := map[string]struct {
		name     string
		port     int
		mutators []consultant.ManagedServiceBuilderMutator
	}{
		"args-no-mutators": {
			name: managedServiceName,
			port: managedServicePort,
		},
		"no-args-mutators": {
			mutators: []consultant.ManagedServiceBuilderMutator{func(builder *consultant.ManagedServiceBuilder) {
				builder.Name = managedServiceName
				builder.Port = managedServicePort
			}},
		},
		"args-mutators": {
			port: managedServicePort,
			mutators: []consultant.ManagedServiceBuilderMutator{func(builder *consultant.ManagedServiceBuilder) {
				builder.Name = managedServiceName
			}},
		},
		"args-mutators-override": {
			name: "something",
			port: 90001,
			mutators: []consultant.ManagedServiceBuilderMutator{func(builder *consultant.ManagedServiceBuilder) {
				builder.Name = managedServiceName
				builder.Port = managedServicePort
			}},
		},
	}
	for name, setup := range tests {
		t.Run(name, func(t *testing.T) {
			b := consultant.NewBareManagedServiceBuilder(setup.name, setup.port, setup.mutators...)
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
			if b.Tags == nil {
				t.Log("Expected Tags to be non-nil")
				t.Fail()
			}
			if b.Checks == nil {
				t.Log("Expected Checks to be non-nil")
				t.Fail()
			}
			if b.TaggedAddresses == nil {
				t.Log("Expected TaggedAddresses to be non-nil")
				t.Fail()
			}
			if b.Meta == nil {
				t.Log("Expected Meta to be non-nil")
				t.Fail()
			}
		})
	}
}
