package util

import (
	"errors"
	"net"
	"os"
)

// These are set on init
var (
	block10  *net.IPNet
	block172 *net.IPNet
	block192 *net.IPNet
)

func init() {
	var err error // simple error holder
	// RFC1918 blocks
	if _, block10, err = net.ParseCIDR("10.0.0.0/8"); err != nil {
		panic(err.Error())
	}
	if _, block172, err = net.ParseCIDR("172.16.0.0/12"); err != nil {
		panic(err.Error())
	}
	if _, block192, err = net.ParseCIDR("192.168.0.0/16"); err != nil {
		panic(err.Error())
	}
}

// MyAddress returns the string output from MyAddressIP, or the error if there was one.
func MyAddress() (string, error) {
	if ip, err := MyAddressIP(); err != nil {
		return "", err
	} else {
		return ip.String(), nil
	}
}

// MyAddressIP searches available interfaces (skip loopback) and returns the first
// private ipv4 address found giving preference to smaller RFC1918 blocks: 192.168.0.0/16 < 172.16.0.0/12 < 10.0.0.0/8
func MyAddressIP() (net.IP, error) {
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
		for _, bl := range []*net.IPNet{block172, block10, block192} {
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
