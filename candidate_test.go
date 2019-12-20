package consultant_test

//
//const (
//	candidateTestKVKey = "consultant/test/candidate-test"
//	candidateTestID    = "test-candidate"
//	candidateLockTTL   = "5s"
//)
//
//type CandidateTestSuite struct {
//	suite.Suite
//
//	server *cst.TestServer
//	client *api.Client
//}
//
//func TestCandidate(t *testing.T) {
//	suite.Run(t, new(CandidateTestSuite))
//}
//
//func (cs *CandidateTestSuite) TearDownTest() {
//	if cs.client != nil {
//		cs.client = nil
//	}
//	if cs.server != nil {
//		cs.server.Stop()
//		cs.server = nil
//	}
//}
//
//func (cs *CandidateTestSuite) TearDownSuite() {
//	cs.TearDownTest()
//}
//
//func (cs *CandidateTestSuite) config(conf *consultant.CandidateConfig) *consultant.CandidateConfig {
//	if conf == nil {
//		conf = new(consultant.CandidateConfig)
//	}
//	conf.Client = cs.client
//	return conf
//}
//
//func (cs *CandidateTestSuite) configKeyed(conf *consultant.CandidateConfig) *consultant.CandidateConfig {
//	conf = cs.config(conf)
//	conf.KVKey = candidateTestKVKey
//	return conf
//}
//
//func (cs *CandidateTestSuite) makeCandidate(num int, conf *consultant.CandidateConfig) *consultant.Candidate {
//	lc := new(consultant.CandidateConfig)
//	if conf != nil {
//		*lc = *conf
//	}
//	lc.CandidateID = fmt.Sprintf("test-%d", num)
//	if lc.SessionTTL == "" {
//		lc.SessionTTL = candidateLockTTL
//	}
//	cand, err := consultant.NewCandidate(cs.configKeyed(lc))
//	if err != nil {
//		cs.T().Fatalf("err: %v", err)
//	}
//
//	return cand
//}
//
//func (cs *CandidateTestSuite) TestNew_EmptyKey() {
//	_, err := consultant.NewCandidate(cs.config(nil))
//	require.NotNil(cs.T(), err, "Expected Empty Key error")
//}
//
//func (cs *CandidateTestSuite) TestNew_EmptyID() {
//	cand, err := consultant.NewCandidate(cs.configKeyed(nil))
//	require.Nil(cs.T(), err, "Error creating candidate: %s", err)
//	if myAddr, err := consultant.LocalAddress(); err != nil {
//		require.NotZero(cs.T(), cand.ID(), "Expected Candidate CandidateID to not be empty")
//	} else {
//		require.Equal(cs.T(), myAddr, cand.ID(), "Expected Candidate CandidateID to be \"%s\", saw \"%s\"", myAddr, cand.ID())
//	}
//}
//
//func (cs *CandidateTestSuite) TestNew_InvalidID() {
//	var err error
//
//	_, err = consultant.NewCandidate(cs.configKeyed(&consultant.CandidateConfig{CandidateID: "thursday dancing in the sun"}))
//	require.Equal(cs.T(), consultant.CandidateInvalidIDErr, err, "Expected \"thursday dancing in the sun\" to return invalid CandidateID error, saw %+v", err)
//
//	_, err = consultant.NewCandidate(cs.configKeyed(&consultant.CandidateConfig{CandidateID: "Große"}))
//	require.Equal(cs.T(), consultant.CandidateInvalidIDErr, err, "Expected \"Große\" to return invalid CandidateID error, saw %+v", err)
//}
//
//func (cs *CandidateTestSuite) TestRun_SimpleElectionCycle() {
//	server, client := makeTestServerAndClient(cs.T(), nil)
//	cs.server = server
//	cs.client = client.Client
//
//	var candidate1, candidate2, candidate3, leaderCandidate *consultant.Candidate
//	var leader *api.SessionEntry
//	var err error
//
//	wg := new(sync.WaitGroup)
//
//	wg.Add(3)
//
//	go func() {
//		candidate1 = cs.makeCandidate(1, &consultant.CandidateConfig{AutoRun: true})
//		candidate1.Wait()
//		wg.Done()
//	}()
//	go func() {
//		candidate2 = cs.makeCandidate(2, &consultant.CandidateConfig{AutoRun: true})
//		candidate2.Wait()
//		wg.Done()
//	}()
//	go func() {
//		candidate3 = cs.makeCandidate(3, &consultant.CandidateConfig{AutoRun: true})
//		candidate3.Wait()
//		wg.Done()
//	}()
//
//	wg.Wait()
//
//	leader, err = candidate1.LeaderService()
//	require.Nil(cs.T(), err, fmt.Sprintf("Unable to locate leader session entry: %v", err))
//
//	// attempt to locate elected leader
//	switch leader.ID {
//	case candidate1.SessionID():
//		leaderCandidate = candidate1
//	case candidate2.SessionID():
//		leaderCandidate = candidate2
//	case candidate3.SessionID():
//		leaderCandidate = candidate3
//	}
//
//	require.NotNil(
//		cs.T(),
//		leaderCandidate,
//		fmt.Sprintf(
//			"Expected one of \"%+v\", saw \"%s\"",
//			[]string{candidate1.SessionID(), candidate2.SessionID(), candidate3.SessionID()},
//			leader.ID))
//
//	leadersFound := 0
//	for i, cand := range []*consultant.Candidate{candidate1, candidate2, candidate3} {
//		if leaderCandidate == cand {
//			leadersFound = 1
//			continue
//		}
//
//		require.True(
//			cs.T(),
//			0 == leadersFound || 1 == leadersFound,
//			fmt.Sprintf("leaderCandidate matched to more than 1 Candidate  Iteration \"%d\"", i))
//
//		require.False(
//			cs.T(),
//			cand.Elected(),
//			fmt.Sprintf("Candidate \"%d\" is not elected but says that it is...", i))
//	}
//
//	wg.Add(3)
//
//	go func() {
//		candidate1.Resign()
//		wg.Done()
//	}()
//	go func() {
//		candidate2.Resign()
//		wg.Done()
//	}()
//	go func() {
//		candidate3.Resign()
//		wg.Done()
//	}()
//
//	wg.Wait()
//
//	leader, err = candidate1.LeaderService()
//	require.NotNil(cs.T(), err, "Expected empty key error, got nil")
//	require.Nil(cs.T(), leader, fmt.Sprintf("Expected nil leader, got %v", leader))
//
//	// election re-enter attempt
//
//	wg.Add(3)
//
//	go func() {
//		candidate1 = cs.makeCandidate(1, &consultant.CandidateConfig{AutoRun: true})
//		candidate1.Wait()
//		wg.Done()
//	}()
//	go func() {
//		candidate2 = cs.makeCandidate(2, &consultant.CandidateConfig{AutoRun: true})
//		candidate2.Wait()
//		wg.Done()
//	}()
//	go func() {
//		candidate3 = cs.makeCandidate(3, &consultant.CandidateConfig{AutoRun: true})
//		candidate3.Wait()
//		wg.Done()
//	}()
//
//	wg.Wait()
//
//	leader, err = candidate1.LeaderService()
//	require.Nil(cs.T(), err, fmt.Sprintf("Unable to locate re-entered leader session entry: %v", err))
//
//	// attempt to locate elected leader
//	switch leader.ID {
//	case candidate1.SessionID():
//		leaderCandidate = candidate1
//	case candidate2.SessionID():
//		leaderCandidate = candidate2
//	case candidate3.SessionID():
//		leaderCandidate = candidate3
//	default:
//		leaderCandidate = nil
//	}
//
//	require.NotNil(
//		cs.T(),
//		leaderCandidate,
//		fmt.Sprintf(
//			"Expected one of \"%+v\", saw \"%s\"",
//			[]string{candidate1.SessionID(), candidate2.SessionID(), candidate3.SessionID()},
//			leader.ID))
//
//	leadersFound = 0
//	for i, cand := range []*consultant.Candidate{candidate1, candidate2, candidate3} {
//		if leaderCandidate == cand {
//			leadersFound = 1
//			continue
//		}
//
//		require.True(
//			cs.T(),
//			0 == leadersFound || 1 == leadersFound,
//			fmt.Sprintf("leaderCandidate matched to more than 1 Candidate  Iteration \"%d\"", i))
//
//		require.False(
//			cs.T(),
//			cand.Elected(),
//			fmt.Sprintf("Candidate \"%d\" is not elected but says that it is...", i))
//	}
//}
//
//func (cs *CandidateTestSuite) TestRun_SessionAnarchy() {
//	server, client := makeTestServerAndClient(cs.T(), nil)
//	cs.server = server
//	cs.client = client.Client
//
//	cand := cs.makeCandidate(1, &consultant.CandidateConfig{AutoRun: true})
//
//	updates := make([]consultant.CandidateUpdate, 0)
//	updatesMu := sync.Mutex{}
//
//	cand.Watch("", func(update consultant.CandidateUpdate) {
//		updatesMu.Lock()
//		cs.T().Logf("Update received: %#v", update)
//		updates = append(updates, update)
//		updatesMu.Unlock()
//	})
//	cand.Wait()
//
//	sid := cand.SessionID()
//	require.NotEmpty(cs.T(), sid, "Expected sid to contain value")
//
//	cs.client.Session().Destroy(sid, nil)
//
//	require.Equal(
//		cs.T(),
//		consultant.CandidateStateRunning,
//		cand.State(),
//		"Expected candidate state to still be %d after session destroyed",
//		consultant.CandidateStateRunning)
//
//	cand.Wait()
//
//	require.NotEmpty(cs.T(), cand.SessionID(), "Expected new session id")
//	require.NotEqual(cs.T(), sid, cand.SessionID(), "Expected new session id")
//
//	updatesMu.Lock()
//	require.Len(cs.T(), updates, 3, "Expected to see 3 updates")
//	updatesMu.Unlock()
//}
//
//type CandidateUtilTestSuite struct {
//	suite.Suite
//
//	server *cst.TestServer
//	client *api.Client
//
//	candidate *consultant.Candidate
//}
//
//func TestCandidate_Util(t *testing.T) {
//	suite.Run(t, &CandidateUtilTestSuite{})
//}
//
//func (us *CandidateUtilTestSuite) SetupSuite() {
//	server, client := makeTestServerAndClient(us.T(), nil)
//	us.server = server
//	us.client = client.Client
//}
//
//func (us *CandidateUtilTestSuite) TearDownSuite() {
//	if us.candidate != nil {
//		us.candidate.Resign()
//	}
//	us.candidate = nil
//	if us.server != nil {
//		us.server.Stop()
//	}
//	us.server = nil
//	us.client = nil
//}
//
//func (us *CandidateUtilTestSuite) TearDownTest() {
//	if us.candidate != nil {
//		us.candidate.Resign()
//	}
//	us.candidate = nil
//}
//
//func (us *CandidateUtilTestSuite) config(conf *consultant.CandidateConfig) *consultant.CandidateConfig {
//	if conf == nil {
//		conf = new(consultant.CandidateConfig)
//	}
//	conf.Client = us.client
//	return conf
//}
//
//func (us *CandidateUtilTestSuite) TestSessionNameParse() {
//	var err error
//
//	myAddr, err := consultant.LocalAddress()
//	if err != nil {
//		us.T().Skipf("Skipping TestSessionNameParse as local addr is indeterminate: %s", err)
//		us.T().SkipNow()
//		return
//	}
//
//	us.candidate, err = consultant.NewCandidate(us.config(&consultant.CandidateConfig{KVKey: candidateTestKVKey, SessionTTL: "10s"}))
//	require.Nil(us.T(), err, "Error creating candidate: %s", err)
//
//	us.candidate.Run()
//
//	err = us.candidate.WaitUntil(20 * time.Second)
//	require.Nil(us.T(), err, "Wait deadline of 20s breached: %s", err)
//
//	ip, err := us.candidate.LeaderIP()
//	require.Nil(us.T(), err, "Error locating Leader Service: %s", err)
//
//	require.Equal(us.T(), myAddr, ip.String(), "Expected Leader IP to be \"%s\", saw \"%s\"", myAddr, ip)
//}
