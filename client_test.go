package consultant_test

//const (
//	clientTestKVKey   = "consultant/tests/junkey"
//	clientTestKVValue = "i don't know what i'handlers doing"
//
//	clientSimpleServiceRegistrationName = "test-service"
//	clientSimpleServiceRegistrationPort = 1234
//)
//
//type ClientTestSuite struct {
//	suite.Suite
//
//	server *cst.TestServer
//	client *consultant.Client
//}
//
//func TestClient(t *testing.T) {
//	suite.Run(t, &ClientTestSuite{})
//}
//
//func (cs *ClientTestSuite) TearDownTest() {
//	if nil != cs.server {
//		cs.server.Stop()
//		cs.server = nil
//	}
//	if nil != cs.client {
//		cs.client = nil
//	}
//}
//
//func (cs *ClientTestSuite) TearDownSuite() {
//	cs.TearDownTest()
//}
//
//func (cs *ClientTestSuite) TestClientConstructionMethods() {
//	var err error
//
//	cs.server = makeTestServer(cs.T(), nil)
//
//	os.Setenv(api.HTTPAddrEnvName, cs.server.HTTPAddr)
//
//	_, err = consultant.NewClient(nil)
//	require.NotNil(cs.T(), err, "Did not see an error when passing \"nil\" to consultant.NewClient()")
//
//	_, err = consultant.NewDefaultClient()
//	require.Nil(cs.T(), err, fmt.Sprintf("Saw error when attmepting to construct default client: %s", err))
//
//	os.Unsetenv(api.HTTPAddrEnvName)
//}
//
//func (cs *ClientTestSuite) TestSimpleClientInteraction() {
//	cs.server, cs.client = makeTestServerAndClient(cs.T(), nil)
//
//	_, err := cs.client.LeaderKV().Put(&api.KVPair{Key: clientTestKVKey, Value: []byte(clientTestKVValue)}, nil)
//	require.Nil(cs.T(), err, fmt.Sprintf("Unable to put key \"%s\": %s", clientTestKVKey, err))
//
//	kv, _, err := cs.client.LeaderKV().Get(clientTestKVKey, nil)
//	require.Nil(cs.T(), err, fmt.Sprintf("Unable to get key \"%s\": %s", clientTestKVKey, err))
//
//	require.NotNil(cs.T(), kv, "LeaderKV was nil")
//	require.IsType(
//		cs.T(),
//		&api.KVPair{},
//		kv,
//		fmt.Sprintf(
//			"Expected LeaderKV Get response to be type \"%s\", saw \"%s\"",
//			reflect.TypeOf(&api.KVPair{}),
//			reflect.TypeOf(kv)))
//}
//
//func (cs *ClientTestSuite) TestSimpleServiceRegister() {
//	cs.server, cs.client = makeTestServerAndClient(cs.T(), nil)
//
//	reg := &consultant.SimpleServiceRegistration{
//		Name: clientSimpleServiceRegistrationName,
//		Port: clientSimpleServiceRegistrationPort,
//	}
//
//	sid, err := cs.client.SimpleServiceRegister(reg)
//	require.Nil(cs.T(), err, fmt.Sprintf("Unable to utilize simple service registration: %s", err))
//
//	svcs, _, err := cs.client.Health().Service(clientSimpleServiceRegistrationName, "", false, nil)
//	require.Nil(cs.T(), err, fmt.Sprintf("Unable to locate service with name \"%s\": %s", clientSimpleServiceRegistrationName, err))
//
//	sidList := make([]string, len(svcs))
//
//	for i, s := range svcs {
//		sidList[i] = s.Service.ID
//	}
//
//	require.Contains(cs.T(), sidList, sid, fmt.Sprintf("Expected to see service id \"%s\" in list \"%+v\"", sid, sidList))
//}
//
//func (cs *ClientTestSuite) TestServiceTagSelection() {
//	cs.server, cs.client = makeTestServerAndClient(cs.T(), nil)
//
//	reg1 := &consultant.SimpleServiceRegistration{
//		Name:     clientSimpleServiceRegistrationName,
//		Port:     clientSimpleServiceRegistrationPort,
//		Tags:     []string{"One", "Two", "Three"},
//		RandomID: true,
//	}
//
//	sid1, err := cs.client.SimpleServiceRegister(reg1)
//	require.Nil(cs.T(), err, "Unable to register service reg1")
//
//	reg2 := &consultant.SimpleServiceRegistration{
//		Name:     clientSimpleServiceRegistrationName,
//		Port:     clientSimpleServiceRegistrationPort,
//		Tags:     []string{"One", "Two"},
//		RandomID: true,
//	}
//	sid2, err := cs.client.SimpleServiceRegister(reg2)
//	require.Nil(cs.T(), err, "Unable to register service reg2")
//
//	reg3 := &consultant.SimpleServiceRegistration{
//		Name:     clientSimpleServiceRegistrationName,
//		Port:     clientSimpleServiceRegistrationPort,
//		Tags:     []string{"One", "Two", "Three", "Three"},
//		RandomID: true,
//	}
//	sid3, err := cs.client.SimpleServiceRegister(reg3)
//	require.Nil(cs.T(), err, "Unable to register service reg3")
//
//	svcs, _, err := cs.client.ServiceByTags(clientSimpleServiceRegistrationName, []string{"One", "Two", "Three"}, consultant.TagsAll, false, nil)
//	require.Nil(cs.T(), err, "error fetching services")
//
//	var sids []string
//	for _, svc := range svcs {
//		sids = append(sids, svc.Service.ID)
//	}
//	require.Contains(cs.T(), sids, sid1, "TagsAll query failed, did not contain", sid1)
//	require.Contains(cs.T(), sids, sid3, "TagsAll query failed, did not contain", sid3)
//	require.NotContains(cs.T(), sids, sid2, "TagsAll query failed, contained", sid2)
//
//	svcs, _, err = cs.client.ServiceByTags(clientSimpleServiceRegistrationName, []string{"One", "Two", "Three", sid1}, consultant.TagsExactly, false, nil)
//	require.Nil(cs.T(), err, "error fetching services")
//
//	sids = nil
//	for _, svc := range svcs {
//		sids = append(sids, svc.Service.ID)
//	}
//	require.Contains(cs.T(), sids, sid1, "TagsExactly query failed first test, did not contain %s", sid1)
//	require.NotContains(cs.T(), sids, sid2, "TagsExactly query failed first test, contained %s", sid2)
//	require.NotContains(cs.T(), sids, sid3, "TagsExactly query failed first test, contained %s", sid3)
//
//	svcs, _, err = cs.client.ServiceByTags(clientSimpleServiceRegistrationName, []string{"One", "Two", "Three", "Three", sid3}, consultant.TagsExactly, false, nil)
//	require.Nil(cs.T(), err, "error fetching services")
//
//	sids = nil
//	for _, svc := range svcs {
//		sids = append(sids, svc.Service.ID)
//	}
//	require.Contains(cs.T(), sids, sid3, "TagsExactly query failed second test (dupes), did not contain %s", sid3)
//	require.NotContains(cs.T(), sids, sid1, "TagsExactly query failed second test (dupes), contained %s", sid1)
//	require.NotContains(cs.T(), sids, sid2, "TagsExactly query failed second test (dupes), contained %s", sid2)
//
//	svcs, _, err = cs.client.ServiceByTags(clientSimpleServiceRegistrationName, []string{"One", "Two", "Three"}, consultant.TagsAny, false, nil)
//	require.Nil(cs.T(), err, "error fetching services")
//
//	sids = nil
//	for _, svc := range svcs {
//		sids = append(sids, svc.Service.ID)
//	}
//	require.Contains(cs.T(), sids, sid1, "TagsAny query failed, did not contain %s", sid1)
//	require.Contains(cs.T(), sids, sid2, "TagsAny query failed, did not contain %s", sid2)
//	require.Contains(cs.T(), sids, sid3, "TagsAny query failed, did not contain %s", sid3)
//
//	svcs, _, err = cs.client.ServiceByTags(clientSimpleServiceRegistrationName, []string{"Three"}, consultant.TagsExclude, false, nil)
//	require.Nil(cs.T(), err, "error fetching services")
//
//	sids = nil
//	for _, svc := range svcs {
//		sids = append(sids, svc.Service.ID)
//	}
//	require.Contains(cs.T(), sids, sid2, "TagsExclude query failed, did not contain %s", sid2)
//	require.NotContains(cs.T(), sids, sid1, "TagsExclude query failed, contained %s", sid1)
//	require.NotContains(cs.T(), sids, sid3, "TagsExclude query failed, contained %s", sid3)
//
//}
//
//func (cs *ClientTestSuite) TestGetServiceAddress_OK() {
//	cs.server, cs.client = makeTestServerAndClient(cs.T(), nil)
//
//	reg := &consultant.SimpleServiceRegistration{
//		Name: clientSimpleServiceRegistrationName,
//		Port: clientSimpleServiceRegistrationPort,
//	}
//
//	_, err := cs.client.SimpleServiceRegister(reg)
//	require.Nil(cs.T(), err, fmt.Sprintf("Unable to utilize simple service registration: %s", err))
//
//	url, err := cs.client.BuildServiceURL("http", clientSimpleServiceRegistrationName, "", false, nil)
//	require.Nil(cs.T(), err, fmt.Sprintf("Error seen while getting service URL: %s", err))
//	require.NotNil(cs.T(), url, fmt.Sprintf("URL was nil.  Saw: %+v", url))
//
//	require.Equal(
//		cs.T(),
//		fmt.Sprintf("%s:%d", cs.client.LocalAddress(), clientSimpleServiceRegistrationPort),
//		url.Host,
//		fmt.Sprintf(
//			"Expected address \"%s:%d\", saw \"%s\"",
//			cs.client.LocalAddress(),
//			clientSimpleServiceRegistrationPort,
//			url.Host))
//}
//
//func (cs *ClientTestSuite) TestGetServiceAddress_Empty() {
//	cs.server, cs.client = makeTestServerAndClient(cs.T(), nil)
//
//	url, err := cs.client.BuildServiceURL("whatever", "nope", "nope", false, nil)
//	require.Nil(cs.T(), url, fmt.Sprintf("URL should be nil, saw %+v", url))
//	require.NotNil(cs.T(), err, "Did not receive expected error message")
//}
//
//func (cs *ClientTestSuite) TestEnsureKey() {
//	cs.server, cs.client = makeTestServerAndClient(cs.T(), nil)
//	_, err := cs.client.LeaderKV().Put(&api.KVPair{Key: "foo/bar", Value: []byte("hello")}, nil)
//	require.Nil(cs.T(), err, fmt.Sprintf("Failed to write key %s : %s", "foo/bar", err))
//
//	kvp, _, err := cs.client.EnsureKey("foo/bar", nil)
//	require.Nil(cs.T(), err, fmt.Sprintf("Failed to read key %s : %s", "foo/bar", err))
//	require.NotNil(cs.T(), kvp, fmt.Sprintf("Key: %s should not be nil, was nil", "/foo/bar"))
//
//	kvp, _, err = cs.client.EnsureKey("foo/barbar", nil)
//	require.NotNil(cs.T(), err, fmt.Sprintf("Non-existent key %s should error, was nil", "foo/barbar"))
//	require.Nil(cs.T(), kvp, fmt.Sprintf("Key: %s should be nil, was not nil", "foo/barbar"))
//}
