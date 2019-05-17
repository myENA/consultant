package consultant_test

import (
	"fmt"
	"github.com/hashicorp/consul/api"
	cst "github.com/hashicorp/consul/sdk/testutil"
	"github.com/myENA/consultant"
	"github.com/myENA/consultant/testutil"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"reflect"
	"testing"
)

const (
	managedServiceName = "managed"
	managedServicePort = 1423

	managedServiceAddedTag = "sandwiches!"
)

type ManagedServiceTestSuite struct {
	suite.Suite

	server *cst.TestServer
	client *consultant.Client
}

func TestManagedService(t *testing.T) {
	suite.Run(t, &ManagedServiceTestSuite{})
}

func (ms *ManagedServiceTestSuite) SetupTest() {
	ms.server, ms.client = testutil.MakeServerAndClient(ms.T(), nil)
}

func (ms *ManagedServiceTestSuite) TearDownTest() {
	if nil != ms.client {
		ms.client = nil
	}

	if nil != ms.server {
		ms.server.Stop()
		ms.server = nil
	}
}

func (ms *ManagedServiceTestSuite) TearDownSuite() {
	ms.TearDownTest()
}

func (ms *ManagedServiceTestSuite) TestManagedServiceCreation() {
	var err error
	var service *consultant.ManagedService
	var svcs []*api.CatalogService
	var tags []string
	var tag string

	service, err = ms.client.ManagedServiceRegister(&consultant.SimpleServiceRegistration{
		Name: managedServiceName,
		Port: managedServicePort,
	})

	require.Nil(ms.T(), err, fmt.Sprintf("Error creating managed service: %s", err))

	require.IsType(
		ms.T(),
		service,
		&consultant.ManagedService{},
		fmt.Sprintf("Expected \"%s\", saw \"%s\"", reflect.TypeOf(&consultant.ManagedService{}), reflect.TypeOf(service)))

	// verify service was added properly
	svcs, _, err = ms.client.Catalog().Service(managedServiceName, "", nil)
	require.Nil(ms.T(), err, fmt.Sprintf("Error looking for service \"%s\": %s", managedServiceName, err))
	require.NotNil(ms.T(), svcs, fmt.Sprintf("Received nil from Catalog().Service(\"%s\")", managedServiceName))
	require.Len(ms.T(), svcs, 1, fmt.Sprintf("Expected exactly 1 service in catalog, saw \"%d\"", len(svcs)))

	// locate service tags
	tags = svcs[0].ServiceTags

	// verify tags is defined and has a length of 1
	require.NotNil(ms.T(), tags, "Service tags are nil")
	require.Len(ms.T(), tags, 1, fmt.Sprintf("Expected exactly 1 tag, saw %v", tags))

	// verify first tag is service id
	tag = tags[0]
	require.Equal(
		ms.T(),
		service.Meta().ID(),
		tag,
		fmt.Sprintf("Expected tag to match \"%s\", saw \"%s\"", service.Meta().ID(), tag))

	// attempt to add new tag
	err = service.AddTags(managedServiceAddedTag)
	require.Nil(ms.T(), err, fmt.Sprintf("Unable to add tag to service \"%s\": %s", managedServiceName, err))

	// verify tag was added
	svcs, _, err = ms.client.Catalog().Service(managedServiceName, "", nil)
	require.Nil(ms.T(), err, fmt.Sprintf("Error looking for service \"%s\": %s", managedServiceName, err))
	require.NotNil(ms.T(), svcs, fmt.Sprintf("Received nil from Catalog().Service(\"%s\")", managedServiceName))
	require.Len(ms.T(), svcs, 1, fmt.Sprintf("Expected exactly 1 service in catalog, saw \"%d\"", len(svcs)))
	tags = svcs[0].ServiceTags
	require.NotNil(ms.T(), tags, "Service tags are nil")
	require.Len(ms.T(), tags, 2, fmt.Sprintf("Expected exactly 2 tags, saw %v", tags))

	// verify added tag is in new list
	require.Contains(ms.T(), tags, managedServiceAddedTag, fmt.Sprintf("Expected tag \"%s\" to be in %v", managedServiceAddedTag, tags))

	// attempt to remove id from tag list
	err = service.RemoveTags(service.Meta().ID())
	require.Nil(ms.T(), err, fmt.Sprintf("Expected nil error, saw \"%s\"", err))

	// verify service id tag was not removed
	svcs, _, err = ms.client.Catalog().Service(managedServiceName, "", nil)
	require.Nil(ms.T(), err, fmt.Sprintf("Error looking for service \"%s\": %s", managedServiceName, err))
	require.NotNil(ms.T(), svcs, fmt.Sprintf("Received nil from Catalog().Service(\"%s\")", managedServiceName))
	require.Len(ms.T(), svcs, 1, fmt.Sprintf("Expected exactly 1 service in catalog, saw \"%d\"", len(svcs)))
	tags = svcs[0].ServiceTags
	require.NotNil(ms.T(), tags, "Service tags are nil")
	require.Len(ms.T(), tags, 2, fmt.Sprintf("Expected exactly 2 tags, saw %v", tags))

	// verify added tag is in new list
	require.Contains(ms.T(), tags, service.Meta().ID(), fmt.Sprintf("Expected tag \"%s\" to be in %v", service.Meta().ID(), tags))

	// attempt to remove added tag
	err = service.RemoveTags(managedServiceAddedTag)
	require.Nil(ms.T(), err, fmt.Sprintf("Error removing tag \"%s\": %s", managedServiceAddedTag, err))

	// verify service id tag was not removed
	svcs, _, err = ms.client.Catalog().Service(managedServiceName, "", nil)
	require.Nil(ms.T(), err, fmt.Sprintf("Error looking for service \"%s\": %s", managedServiceName, err))
	require.NotNil(ms.T(), svcs, fmt.Sprintf("Received nil from Catalog().Service(\"%s\")", managedServiceName))
	require.Len(ms.T(), svcs, 1, fmt.Sprintf("Expected exactly 1 service in catalog, saw \"%d\"", len(svcs)))
	tags = svcs[0].ServiceTags
	require.NotNil(ms.T(), tags, "Service tags are nil")
	require.Len(ms.T(), tags, 1, fmt.Sprintf("Expected exactly 1 tags, saw %v", tags))

	// verify first tag is service id
	tag = tags[0]
	require.Equal(
		ms.T(),
		service.Meta().ID(),
		tag,
		fmt.Sprintf("Expected tag to match \"%s\", saw \"%s\"", service.Meta().ID(), tag))
}
