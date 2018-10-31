package session

import (
	"fmt"
	"strings"
)

type NameParts struct {
	Key      string
	NodeName string
	RandomID string
}

// ParseName is provided so you don't have to parse it yourself :)
func ParseName(name string) (*NameParts, error) {
	split := strings.Split(name, "_")

	switch len(split) {
	case 2:
		return &NameParts{
			NodeName: split[0],
			RandomID: split[1],
		}, nil
	case 3:
		return &NameParts{
			Key:      split[0],
			NodeName: split[1],
			RandomID: split[2],
		}, nil

	default:
		return nil, fmt.Errorf("expected 2 or 3 parts in session name \"%s\", saw only \"%d\"", name, len(split))
	}
}
