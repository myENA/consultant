package consultant

import (
	"errors"
	"fmt"
	"math/rand"
	"net"
	"os"
	"sort"
	"strings"

	"github.com/hashicorp/consul/api"
)

const (
	rnb = "0123456789"
	rlb = "abcdefghijklmnopqrstuvwxyz"
	rLb = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"

	rnbl = int64(10)
	rlbl = int64(26)
	rLbl = int64(26)

	notFoundErrPrefix = "Unexpected response code: 404 ("
)

// These are set on init
var (
	localAddressBlock10  *net.IPNet
	localAddressBlock172 *net.IPNet
	localAddressBlock192 *net.IPNet
)

func init() {
	var err error // simple error holder
	// RFC1918 blocks
	if _, localAddressBlock10, err = net.ParseCIDR("10.0.0.0/8"); err != nil {
		panic(err.Error())
	}
	if _, localAddressBlock172, err = net.ParseCIDR("172.16.0.0/12"); err != nil {
		panic(err.Error())
	}
	if _, localAddressBlock192, err = net.ParseCIDR("192.168.0.0/16"); err != nil {
		panic(err.Error())
	}
}

// LocalAddress returns the string output from LocalAddressIP, or the error if there was one.
func LocalAddress() (string, error) {
	if ip, err := LocalAddressIP(); err != nil {
		return "", err
	} else {
		return ip.String(), nil
	}
}

// LocalAddressIP searches available interfaces (skip loopback) and returns the first
// private ipv4 address found giving preference to smaller RFC1918 blocks: 192.168.0.0/16 < 172.16.0.0/12 < 10.0.0.0/8
func LocalAddressIP() (net.IP, error) {
	envAddr := os.Getenv("CONSUL_SERVICE_ADDR")
	if envAddr != "" {
		return net.ParseIP(envAddr), nil
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	myInterface := os.Getenv("CONSUL_SERVICE_INTERFACE")

	for _, iface := range ifaces {
		// We are looking for a specific interface and only that one will be considered
		if myInterface != "" && iface.Name != myInterface {
			continue
		}

		// We looked for an interface name and we found it
		addrs, err := iface.Addrs()
		if err != nil {
			return nil, err
		}

		// Look for interfaces in a list of prioritized netblocks
		for _, bl := range []*net.IPNet{localAddressBlock172, localAddressBlock10, localAddressBlock192} {
			for _, addr := range addrs {
				// bit kludgy to go via the CIDR but see no other way
				cidr := addr.String()
				ip, _, err := net.ParseCIDR(cidr)
				if err != nil {
					return nil, err
				}
				// don't report loopback or ipv6 addresses
				if !ip.IsLoopback() && ip.To4() != nil {
					if bl.Contains(ip) {
						return ip, nil
					}
				}
			}
		}
	}

	return nil, errors.New("no valid interfaces found")
}

// LazyRandomString will create a base62 string with a min-length of 12
func LazyRandomString(n int) string {
	if n <= 0 {
		panic(fmt.Sprintf("n must be > 0, saw %d", n))
	}
	buff := make([]byte, n)
	for i := 0; i < n; i++ {
		switch rand.Intn(3) {
		case 0:
			buff[i] = rnb[rand.Int63()%rnbl]
		case 1:
			buff[i] = rlb[rand.Int63()%rlbl]
		case 2:
			buff[i] = rLb[rand.Int63()%rLbl]
		}
	}
	return string(buff)
}

// RandomLocalPort is a very lazy way to ask for a random port from the kernel.  Probably don't rely on this.
//
// shamelessly copy-pasted from and old version of consul test util
func RandomLocalPort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

// determines if a and b contain the same elements (order doesn't matter)
func strSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	ac := make([]string, len(a))
	bc := make([]string, len(b))
	copy(ac, a)
	copy(bc, b)
	sort.Strings(ac)
	sort.Strings(bc)
	for i, v := range ac {
		if bc[i] != v {
			return false
		}
	}
	return true
}

// SpecificServiceEntry attempts to find a specific service's entry from the health endpoint
func SpecificServiceEntry(serviceID string, svcs []*api.ServiceEntry) (*api.ServiceEntry, bool) {
	for _, svc := range svcs {
		if svc.Service.ID == serviceID {
			return svc, true
		}
	}
	return nil, false
}

// SpecificCatalogService attempts to find a specific service's entry from the catalog endpoint
func SpecificCatalogService(serviceID string, svcs []*api.CatalogService) (*api.CatalogService, bool) {
	for _, svc := range svcs {
		if svc.ServiceID == serviceID {
			return svc, true
		}
	}
	return nil, false
}

// SpecificChecks attempts to find a specific service's checks from the health check endpoint
func SpecificChecks(serviceID string, checks api.HealthChecks) api.HealthChecks {
	myChecks := make(api.HealthChecks, 0)
	for _, check := range checks {
		if check.ServiceID == serviceID {
			myChecks = append(myChecks, check)
		}
	}
	return myChecks
}

// IsNotFoundErr performs a simple test to see if the provided error describes a "404 not found" response from an agent
func IsNotFoundError(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), notFoundErrPrefix)
}
