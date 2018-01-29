package consultant

import "github.com/myENA/consultant/util"

// GetMyAddress will attempt to locate your local RFC-1918 address
//
// DEPRECATED: Please use util.MyAddress()
func GetMyAddress() (string, error) {
	return util.MyAddress()
}
