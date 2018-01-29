package candidate

import (
	"fmt"
	"github.com/myENA/consultant/candidate/session"
	"strings"
)

type SessionNameParts struct {
	session.NameParts
	CandidateID string
}

func ParseSessionName(name string) (*SessionNameParts, error) {
	sn, err := session.ParseName(name)
	if err != nil {
		return nil, err
	}

	if !strings.HasPrefix(sn.Key, SessionKeyPrefix) {
		return nil, fmt.Errorf("session key \"%s\" does not have expected prefix \"%s\"", sn.Key, SessionKeyPrefix)
	}

	return &SessionNameParts{
		NameParts:   *sn,
		CandidateID: sn.Key[len(SessionKeyPrefix):],
	}, nil
}
