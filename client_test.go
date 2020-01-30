package consultant_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/consul/api"
	"github.com/myENA/consultant/v2"
)

const (
	clientTestKVKey   = "consultant/tests/junkey"
	clientTestKVValue = "i don't know what i'm doing"

	clientSimpleServiceRegistrationName = "test-service"
	clientSimpleServiceRegistrationPort = 1234
)

func TestNewClient(t *testing.T) {
	t.Run("nil-config", func(t *testing.T) {
		if _, err := consultant.NewClient(nil); err == nil {
			t.Logf("Did not see an error when passing \"nil\" to consultant.NewClient()")
			t.Fail()
		}
	})
}

func TestNewDefaultClient(t *testing.T) {
	server := makeTestServer(t, nil)
	server.WaitForSerfCheck(t)
	server.WaitForLeader(t)
	defer stopTestServer(server)

	t.Run("set-env", func(t *testing.T) {
		if err := os.Setenv(api.HTTPAddrEnvName, server.HTTPAddr); err != nil {
			t.Logf("error setting %q: %s", api.HTTPAddrEnvName, err)
			t.Fail()
		}
	})

	t.Run("construct", func(t *testing.T) {
		if _, err := consultant.NewDefaultClient(); err != nil {
			t.Logf("Saw error when attmepting to construct default client: %s", err)
			t.Fail()
		}
	})

	t.Run("unset-env", func(t *testing.T) {
		if err := os.Unsetenv(api.HTTPAddrEnvName); err != nil {
			t.Logf("error unsetting %q: %s", api.HTTPAddrEnvName, err)
			t.Fail()
		}
	})
}

func TestClient_SimpleServiceRegister(t *testing.T) {
	server, client := makeTestServerAndClient(t, nil)
	server.WaitForSerfCheck(t)
	server.WaitForLeader(t)
	defer stopTestServer(server)

	var sid string

	t.Run("register", func(t *testing.T) {
		var err error
		reg := &consultant.SimpleServiceRegistration{
			Name: clientSimpleServiceRegistrationName,
			Port: clientSimpleServiceRegistrationPort,
		}
		if sid, err = client.SimpleServiceRegister(reg); err != nil {
			t.Logf("Unable to utilize simple service registration: %s", err)
			t.Fail()
		}
	})

	if t.Failed() {
		return
	}

	t.Run("locate", func(t *testing.T) {
		svcs, _, err := client.Health().Service(clientSimpleServiceRegistrationName, "", false, nil)
		if err != nil {
			t.Logf("Error fetching service: %s", err)
			t.Fail()
		} else if len(svcs) == 0 {
			t.Log("Empty services list returned")
			t.Fail()
		} else {
			for _, svc := range svcs {
				if svc.Service.ID == sid {
					return
				}
			}
			t.Logf("Registered service not found in list: %v", svcs)
			t.Fail()
		}
	})
}

func TestClient_PickServiceMultipleTags(t *testing.T) {
	server, client := makeTestServerAndClient(t, nil)
	server.WaitForSerfCheck(t)
	server.WaitForLeader(t)
	defer stopTestServer(server)

	var (
		sid1 string
		sid2 string
		sid3 string
	)

	findServices := func(t *testing.T, svcs []*api.ServiceEntry) (bool, bool, bool, []string) {
		var (
			svc1 bool
			svc2 bool
			svc3 bool

			sids = make([]string, 0)
		)

		for _, svc := range svcs {
			sids = append(sids, svc.Service.ID)

			switch svc.Service.ID {
			case sid1:
				svc1 = true
			case sid2:
				svc2 = true
			case sid3:
				svc3 = true

			default:
				t.Logf("Unexpected service found: %v", svc)
				t.Fail()
			}
		}

		return svc1, svc2, svc3, sids
	}

	t.Run("register-svc1", func(t *testing.T) {
		var err error
		reg := &consultant.SimpleServiceRegistration{
			Name:     clientSimpleServiceRegistrationName,
			Port:     clientSimpleServiceRegistrationPort,
			Tags:     []string{"One", "Two", "Three"},
			RandomID: true,
		}
		if sid1, err = client.SimpleServiceRegister(reg); err != nil {
			t.Logf("Error registering service: %s", err)
			t.Fail()
		}
	})

	if t.Failed() {
		return
	}

	t.Run("register-svc2", func(t *testing.T) {
		var err error
		reg := &consultant.SimpleServiceRegistration{
			Name:     clientSimpleServiceRegistrationName,
			Port:     clientSimpleServiceRegistrationPort,
			Tags:     []string{"One", "Two"},
			RandomID: true,
		}
		if sid2, err = client.SimpleServiceRegister(reg); err != nil {
			t.Logf("Error registering service: %s", err)
			t.Fail()
		}
	})

	if t.Failed() {
		return
	}

	t.Run("register-svc3", func(t *testing.T) {
		var err error
		reg := &consultant.SimpleServiceRegistration{
			Name:     clientSimpleServiceRegistrationName,
			Port:     clientSimpleServiceRegistrationPort,
			Tags:     []string{"One", "Two", "Three", "Three"},
			RandomID: true,
		}
		if sid3, err = client.SimpleServiceRegister(reg); err != nil {
			t.Logf("Error registering service: %s", err)
			t.Fail()
		}
	})

	if t.Failed() {
		return
	}

	t.Run("find-all-one-two-three", func(t *testing.T) {
		svcs, _, err := client.ServiceByTags(clientSimpleServiceRegistrationName, []string{"One", "Two", "Three"}, consultant.TagsAll, false, nil)
		if err != nil {
			t.Logf("Error fetching services: %s", err)
			t.Fail()
			return
		}

		svc1, svc2, svc3, sids := findServices(t, svcs)
		if t.Failed() {
			return
		}

		if !svc1 || !svc3 {
			t.Logf("Services list did not contain %q and/or %q: %v", sid1, sid3, sids)
			t.Fail()
		}

		if svc2 {
			t.Logf("Services list should not have contained %q: %v", sid2, sids)
			t.Fail()
		}
	})

	t.Run("find-exactly-one-two-three", func(t *testing.T) {
		svcs, _, err := client.ServiceByTags(clientSimpleServiceRegistrationName, []string{"One", "Two", "Three", sid1}, consultant.TagsExactly, false, nil)
		if err != nil {
			t.Logf("Error fetching services: %s", err)
			t.Fail()
			return
		}

		if len(svcs) == 0 {
			t.Log("No services returned")
			t.Fail()
			return
		}

		svc1, svc2, svc3, sids := findServices(t, svcs)
		if t.Failed() {
			return
		}

		if !svc1 {
			t.Logf("Services list should have contained %q: %v", sid1, sids)
			t.Fail()
		}

		if svc2 || svc3 {
			t.Logf("Services list should not have contained %q or %q: %v", sid2, sid3, sids)
			t.Fail()
		}
	})

	t.Run("find-exactly-one-two-three-sid3", func(t *testing.T) {
		svcs, _, err := client.ServiceByTags(clientSimpleServiceRegistrationName, []string{"One", "Two", "Three", "Three", sid3}, consultant.TagsExactly, false, nil)
		if err != nil {
			t.Logf("Error fetching services: %s", err)
			t.Fail()
			return
		}

		if len(svcs) == 0 {
			t.Logf("No services returned")
			t.Fail()
			return
		}

		svc1, svc2, svc3, sids := findServices(t, svcs)
		if t.Failed() {
			return
		}

		if svc1 || svc2 {
			t.Logf("Services list should not have contained %q or %q: %v", sid1, sid2, sids)
			t.Fail()
		}

		if !svc3 {
			t.Logf("Services list should have contained %q: %v", sid3, sids)
			t.Fail()
		}
	})

	t.Run("find-any-one-two-three", func(t *testing.T) {
		svcs, _, err := client.ServiceByTags(clientSimpleServiceRegistrationName, []string{"One", "Two", "Three"}, consultant.TagsAny, false, nil)
		if err != nil {
			t.Logf("Error fetching services: %s", err)
			t.Fail()
			return
		}

		if len(svcs) == 0 {
			t.Logf("No services returned")
			t.Fail()
			return
		}

		svc1, svc2, svc3, sids := findServices(t, svcs)
		if t.Failed() {
			return
		}

		if !svc1 || !svc2 || !svc3 {
			t.Logf("Services list should have contained %q and %q and %q: %v", sid1, sid2, sid3, sids)
			t.Fail()
		}
	})

	t.Run("find-exclude-three", func(t *testing.T) {
		svcs, _, err := client.ServiceByTags(clientSimpleServiceRegistrationName, []string{"Three"}, consultant.TagsExclude, false, nil)
		if err != nil {
			t.Logf("Error fetching services: %s", err)
			t.Fail()
			return
		}

		if len(svcs) == 0 {
			t.Log("No services returned")
			t.Fail()
			return
		}

		svc1, svc2, svc3, sids := findServices(t, svcs)
		if t.Failed() {
			return
		}

		if svc1 || svc3 {
			t.Logf("Services list should not have contained %q or %q: %v", sid1, sid3, sids)
			t.Fail()
		}

		if !svc2 {
			t.Logf("Services list should have contained %q: %v", sid2, sids)
			t.Fail()
		}
	})
}

func TestClient_BuildServiceURL(t *testing.T) {
	server, client := makeTestServerAndClient(t, nil)
	server.WaitForSerfCheck(t)
	server.WaitForLeader(t)
	defer stopTestServer(server)

	t.Run("register", func(t *testing.T) {
		reg := &consultant.SimpleServiceRegistration{
			Name: clientSimpleServiceRegistrationName,
			Port: clientSimpleServiceRegistrationPort,
		}

		if _, err := client.SimpleServiceRegister(reg); err != nil {
			t.Logf("Error registering service: %s", err)
			t.Fail()
		}
	})

	if t.Failed() {
		return
	}

	t.Run("success", func(t *testing.T) {
		expected := fmt.Sprintf("%s:%d", client.LocalAddress(), clientSimpleServiceRegistrationPort)

		if url, err := client.BuildServiceURL("http", clientSimpleServiceRegistrationName, "", false, nil); err != nil {
			t.Logf("Error building service url: %s", err)
			t.Fail()
		} else if url == nil {
			t.Log("Url was nil")
			t.Fail()
		} else if actual := url.Host; expected != actual {
			t.Logf("Expected build url to be %q, saw %q", expected, actual)
			t.Fail()
		}
	})

	t.Run("empty", func(t *testing.T) {
		if url, err := client.BuildServiceURL("whatever", "nope", "nope", false, nil); err == nil {
			t.Log("Expected error")
			t.Fail()
		} else if url != nil {
			t.Logf("Expected url to be nil, saw %v", url)
			t.Fail()
		}
	})
}

func TestClient_EnsureKey(t *testing.T) {
	server, client := makeTestServerAndClient(t, nil)
	server.WaitForSerfCheck(t)
	server.WaitForLeader(t)
	defer stopTestServer(server)

	t.Run("put-key", func(t *testing.T) {
		if _, err := client.KV().Put(&api.KVPair{Key: "foo/bar", Value: []byte("hello")}, nil); err != nil {
			t.Logf("Error putting key: %s", err)
			t.Fail()
		}
	})

	if t.Failed() {
		return
	}

	t.Run("get-success", func(t *testing.T) {
		if kvp, _, err := client.EnsureKey("foo/bar", nil); err != nil {
			t.Logf("Error fetching key: %s", err)
			t.Fail()
		} else if kvp == nil {
			t.Log("kvp should not be nil")
			t.Fail()
		}
	})

	t.Run("get-error", func(t *testing.T) {
		if kvp, _, err := client.EnsureKey("foo/barbar", nil); err == nil {
			t.Log("Expected err to be not nil")
			t.Fail()
		} else if kvp != nil {
			t.Logf("Expected kvp to be nil: %v", kvp)
			t.Fail()
		}
	})
}
