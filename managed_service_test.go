package consultant_test

import (
	"testing"

	cst "github.com/hashicorp/consul/testutil"
	"github.com/myENA/consultant"
	"github.com/myENA/consultant/testutil"
)

const (
	managedServiceName = "managed"
	managedServicePort = 1423

	managedServiceAddedTag = "sandwiches!"
)

func TestManagedServiceCreation(t *testing.T) {
	var (
		service *consultant.ManagedService
		server  *cst.TestServer
		client  *consultant.Client
	)

	t.Run("Setup", func(t *testing.T) {
		var err error
		// create server and client
		server, client = testutil.MakeServerAndClient(t, nil)

		// register service
		service, err = client.ManagedServiceRegister(&consultant.SimpleServiceRegistration{
			Name: managedServiceName,
			Port: managedServicePort,
		})
		if err != nil {
			t.Logf("Error creating client: %s", err)
			t.FailNow()
		}

		// verify service was added properly
		svcs, _, err := client.Catalog().Service(managedServiceName, "", nil)
		if err != nil {
			t.Logf("Error fetching service list: %s", err)
			t.FailNow()
		}
		if len(svcs) != 1 {
			t.Logf("Expected exactly 1 service in catalog, saw:  %+v", svcs)
			t.FailNow()
		}

		// locate service tags
		tags := svcs[0].ServiceTags
		if len(tags) != 1 {
			t.Logf("Expected exactly 1 tag on service, saw: %+v", tags)
			t.FailNow()
		}
		tag := tags[0]
		if tag != service.Meta().ID() {
			t.Logf("Expected first tag to be %q, saw: %+v", service.Meta().ID(), tag)
			t.FailNow()
		}
	})

	t.Run("MutateTags_Add", func(t *testing.T) {
		var err error
		// attempt to add new tag
		err = service.AddTags(managedServiceAddedTag)
		if err != nil {
			t.Logf("Error adding tags: %s", err)
			t.FailNow()
		}

		// refetch service
		svcs, _, err := client.Catalog().Service(managedServiceName, "", nil)
		if err != nil {
			t.Logf("Error fetching services: %s", err)
			t.FailNow()
		}
		if len(svcs) != 1 {
			t.Logf("Expected exactly 1 service, saw: %+v", svcs)
			t.FailNow()
		}

		// check for existence
		tags := svcs[0].ServiceTags
		if len(tags) != 2 {
			t.Logf("Expected exactly 2 tags, saw: %+v", tags)
			t.FailNow()
		}
		found := false
		for _, tag := range tags {
			if tag == managedServiceAddedTag {
				found = true
				break
			}
		}
		if !found {
			t.Logf("Expected to find tag %q in list, saw: %+v", managedServiceAddedTag, tags)
			t.FailNow()
		}
	})

	t.Run("MutateTags_CannotRemoveIDTag", func(t *testing.T) {
		var err error
		err = service.RemoveTags(service.Meta().ID())
		if err != nil {
			t.Logf("Error removing tag: %s", err)
			t.FailNow()
		}

		// verify service id tag was not removed
		svcs, _, err := client.Catalog().Service(managedServiceName, "", nil)
		if err != nil {
			t.Logf("Error fetching services: %s", err)
			t.FailNow()
		}
		if len(svcs) != 1 {
			t.Logf("Expected exactly 1 service, saw: %+v", svcs)
			t.FailNow()
		}

		tags := svcs[0].ServiceTags
		if len(tags) != 2 {
			t.Logf("Expected exactly 2 tags, saw: %+v", tags)
			t.FailNow()
		}

		found := false
		for _, tag := range tags {
			if tag == service.Meta().ID() {
				found = true
				break
			}
		}
		if !found {
			t.Logf("Expected to see %q in tag list, saw: %+v", service.Meta().ID(), tags)
			t.FailNow()
		}
	})

	t.Run("MutateTags_RemoveArbitraryTag", func(t *testing.T) {
		var err error
		err = service.RemoveTags(managedServiceAddedTag)
		if err != nil {
			t.Logf("Error removing tag: %s", err)
			t.FailNow()
		}

		// verify tag was removed
		svcs, _, err := client.Catalog().Service(managedServiceName, "", nil)
		if err != nil {
			t.Logf("Error fetching services: %s", err)
			t.FailNow()
		}
		if len(svcs) != 1 {
			t.Logf("Expected exactly 1 service, saw: %+v", svcs)
			t.FailNow()
		}

		tags := svcs[0].ServiceTags
		if len(tags) != 1 {
			t.Logf("Expected exactly 1 tags, saw: %+v", tags)
			t.FailNow()
		}

		for _, tag := range tags {
			if tag == managedServiceAddedTag {
				t.Logf("Expected %q to be removed from tag list, saw: %+v", managedServiceAddedTag, tags)
				t.FailNow()
			}
		}
	})

	if server != nil {
		server.Stop()
	}
}
