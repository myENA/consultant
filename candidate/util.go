package candidate

import (
	"fmt"
	"strings"
)

type SessionParts struct {
	Prefix     string
	ID         string
	NodeName   string
	RandomUUID string
}

// ParseCandidateSessionName is provided so you don't have to parse it yourself :)
func ParseSessionName(name string) (*SessionParts, error) {
	split := strings.Split(name, "-")
	if 4 != len(split) {
		return nil, fmt.Errorf("expected four parts in session name \"%s\", saw only \"%d\"", name, len(split))
	}

	return &SessionParts{
		Prefix:     split[0],
		ID:         split[1],
		NodeName:   split[2],
		RandomUUID: split[3],
	}, nil
}
