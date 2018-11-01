package consultant_test

import (
	"fmt"
	"github.com/hashicorp/consul/api"
	"github.com/myENA/consultant"
	"github.com/myENA/consultant/testutil"
	"os"
	"testing"
)

const (
	clientTestKVKey   = "consultant/tests/junkey"
	clientTestKVValue = "i don't know what i'm doing"

	clientSimpleServiceRegistrationName = "test-service"
	clientSimpleServiceRegistrationPort = 1234
)

func TestClientConstructionMethods(t *testing.T) {
	server := testutil.MakeServer(t, nil)
	os.Setenv(api.HTTPAddrEnvName, server.HTTPAddr)

	t.Run("NewClient", func(t *testing.T) {
		if _, err := consultant.NewClient(nil); err != nil {
			t.Logf("NewClient err: %s", err)
			t.FailNow()
		}
	})
	t.Run("NewDefaultClient", func(t *testing.T) {
		if _, err := consultant.NewDefaultClient(); err != nil {
			t.Logf("NewDefaultClient err: %s", err)
			t.FailNow()
		}
	})

	os.Unsetenv(api.HTTPAddrEnvName)
	server.Stop()
}

func TestSimpleClientInteraction(t *testing.T) {
	server, client := testutil.MakeServerAndClient(t, nil)

	t.Run("PutKV", func(t *testing.T) {
		if _, err := client.KV().Put(&api.KVPair{Key: clientTestKVKey, Value: []byte(clientTestKVValue)}, nil); err != nil {
			t.Logf("PutKV err: %s", err)
			t.FailNow()
		}
	})
	t.Run("GetKV", func(t *testing.T) {
		if kv, _, err := client.KV().Get(clientTestKVKey, nil); err != nil {
			t.Logf("GetKV err : %s", err)
			t.FailNow()
		} else if kv == nil {
			t.Log("KV was nil")
			t.FailNow()
		}
	})

	server.Stop()
}

func TestSimpleServiceRegister(t *testing.T) {
	server, client := testutil.MakeServerAndClient(t, nil)

	reg := &consultant.SimpleServiceRegistration{
		Name: clientSimpleServiceRegistrationName,
		Port: clientSimpleServiceRegistrationPort,
	}

	var (
		// TODO: this is probably a terrible idea...
		sid string
		err error
	)

	t.Run("RegisterService", func(t *testing.T) {
		if sid, err = client.SimpleServiceRegister(reg); err != nil {
			t.Logf("Failed register service: %s", err)
			t.FailNow()
		}
	})
	t.Run("VerifyRegister", func(t *testing.T) {
		svcs, _, err := client.Health().Service(clientSimpleServiceRegistrationName, "", false, nil)
		if err != nil {
			t.Logf("Error fetching services: %s", err)
			t.FailNow()
		}
		var found bool
		for _, svc := range svcs {
			if svc.Service.ID == sid {
				found = true
				break
			}
		}
		if !found {
			t.Logf("Unable to locate service ID %q in list %+v", sid, svcs)
		}
	})

	server.Stop()
}

func TestServiceTagSelection(t *testing.T) {
	var (
		sid1, sid2, sid3 string
		err              error
	)

	server, client := testutil.MakeServerAndClient(t, nil)

	reg1 := &consultant.SimpleServiceRegistration{
		Name:     clientSimpleServiceRegistrationName,
		Port:     clientSimpleServiceRegistrationPort,
		Tags:     []string{"One", "Two", "Three"},
		RandomID: true,
	}
	if sid1, err = client.SimpleServiceRegister(reg1); err != nil {
		t.Logf("Unable to register: %s", err)
		t.FailNow()
	}
	reg2 := &consultant.SimpleServiceRegistration{
		Name:     clientSimpleServiceRegistrationName,
		Port:     clientSimpleServiceRegistrationPort,
		Tags:     []string{"One", "Two"},
		RandomID: true,
	}
	if sid2, err = client.SimpleServiceRegister(reg2); err != nil {
		t.Logf("Unable to register: %s", err)
		t.FailNow()
	}
	reg3 := &consultant.SimpleServiceRegistration{
		Name:     clientSimpleServiceRegistrationName,
		Port:     clientSimpleServiceRegistrationPort,
		Tags:     []string{"One", "Two", "Three", "Three"},
		RandomID: true,
	}
	if sid3, err = client.SimpleServiceRegister(reg3); err != nil {
		t.Logf("Unable to register: %s", err)
		t.FailNow()
	}

	tests := map[string]struct {
		tags     []string
		flag     consultant.TagsOption
		expected []string
	}{
		"TagsAll": {
			[]string{"One", "Two", "Three"},
			consultant.TagsAll,
			[]string{sid1, sid3},
		},
		"TagsExactly_1": {
			[]string{"One", "Two", "Three", sid1},
			consultant.TagsExactly,
			[]string{sid1},
		},
		"TagsExactly_3": {
			[]string{"One", "Two", "Three", "Three", sid3},
			consultant.TagsExactly,
			[]string{sid3},
		},
		"TagsAny": {
			[]string{"One", "Two", "Three"},
			consultant.TagsAny,
			[]string{sid1, sid2, sid3},
		},
		"TagsExclude": {
			[]string{"Three"},
			consultant.TagsExclude,
			[]string{sid2},
		},
	}

	for name, test := range tests {
		name, test := name, test
		t.Run(name, func(t *testing.T) {
			svcs, _, err := client.ServiceByTags(clientSimpleServiceRegistrationName, test.tags, test.flag, false, nil)
			if err != nil {
				t.Logf("Unable to fetch services: %s", err)
				t.FailNow()
			} else {
				sids := make([]string, 0)
				for _, svc := range svcs {
					sids = append(sids, svc.Service.ID)
				}
			Expected:
				for _, expected := range test.expected {
					for _, sid := range sids {
						if expected == sid {
							continue Expected
						}
					}
					t.Logf("%s failed, expected sid %q not found in list %+v", name, expected, sids)
					t.Fail()
				}
				if len(sids) != len(test.expected) {
					t.Logf("%s failed, expected %+v, saw %+v", name, test.expected, sids)
				}
			}
		})
	}

	server.Stop()
}

func TestGetServiceAddress(t *testing.T) {
	server, client := testutil.MakeServerAndClient(t, nil)

	reg := &consultant.SimpleServiceRegistration{
		Name: clientSimpleServiceRegistrationName,
		Port: clientSimpleServiceRegistrationPort,
	}

	_, err := client.SimpleServiceRegister(reg)
	if err != nil {
		t.Logf("Failed to register service: %s", err)
		t.FailNow()
	}

	t.Run("BuildServiceURL", func(t *testing.T) {
		url, err := client.BuildServiceURL("http", clientSimpleServiceRegistrationName, "", false, nil)
		if err != nil {
			t.Logf("Error building service url: %s", err)
			t.FailNow()
		} else if url == nil {
			t.Log("url was nil")
			t.FailNow()
		} else {
			expected := fmt.Sprintf("%s:%d", client.MyAddr(), clientSimpleServiceRegistrationPort)
			if url.Host != expected {
				t.Logf("Expected address \"%s:%d\", saw \"%s\"", client.MyAddr(), clientSimpleServiceRegistrationPort, url.Host)
				t.FailNow()
			}
		}
	})

	server.Stop()
}

func TestGetServiceAddress_Empty(t *testing.T) {
	server, client := testutil.MakeServerAndClient(t, nil)

	url, err := client.BuildServiceURL("whatever", "nope", "nope", false, nil)
	if err == nil {
		t.Log("Expected error to be returned")
		t.FailNow()
	}
	if url != nil {
		t.Logf("Expected url to be nil, saw %+v", url)
		t.FailNow()
	}

	server.Stop()
}

func TestEnsureKey(t *testing.T) {
	server, client := testutil.MakeServerAndClient(t, nil)

	if _, err := client.KV().Put(&api.KVPair{Key: "foo/bar", Value: []byte("hello")}, nil); err != nil {
		t.Logf("Error putting key: %s", err)
		t.FailNow()
	}

	t.Run("EnsureKey_exists", func(t *testing.T) {
		if kvp, _, err := client.EnsureKey("foo/bar", nil); err != nil {
			t.Logf("Failed to read key %s : %s", "foo/bar", err)
			t.FailNow()
		} else if kvp == nil {
			t.Logf("Key: %s should not be nil, was nil", "/foo/bar")
			t.FailNow()
		}
	})

	t.Run("EnsureKey_notexists", func(t *testing.T) {
		if kvp, _, err := client.EnsureKey("foo/barbar", nil); err == nil {
			t.Logf("Non-existent key %s should error, was nil", "foo/barbar")
			t.FailNow()
		} else if kvp != nil {
			t.Logf("Key: %s should be nil, was not nil", "foo/barbar")
			t.FailNow()
		}
	})

	server.Stop()
}
