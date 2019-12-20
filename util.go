package consultant

import (
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/consul/api"
)

const (
	// EnvConsulLocalAddr is an environment variable you may define that will be used for LocalAddress() results
	EnvConsulLocalAddr = "CONSUL_LOCAL_ADDR"
	// EnvConsulLocalInterface is an environment variable you may define to limit the interfaces looped over to find
	// a RFC1918 address on
	EnvConsulLocalInterface = "CONSUL_LOCAL_INTERFACE"

	SlugRand = "!RAND!"
	SlugName = "!NAME!"
	SlugAddr = "!ADDR!"
	SlugNode = "!NODE!"
	SlugUnix = "!UNIX!"

	rnb = "0123456789"
	rlb = "abcdefghijklmnopqrstuvwxyz"
	rLb = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"

	rnbl = int64(10)
	rlbl = int64(26)
	rLbl = int64(26)

	notFoundErrPrefix = "Unexpected response code: 404 ("

	defaultInternalRequestTTL = 2 * time.Second
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
		panic(fmt.Sprintf("error parsing cidir 10.0.0.0/8: %s", err))
	}
	if _, localAddressBlock172, err = net.ParseCIDR("172.16.0.0/12"); err != nil {
		panic(fmt.Sprintf("error parsing cidr 172.16.0.0/12: %s", err))
	}
	if _, localAddressBlock192, err = net.ParseCIDR("192.168.0.0/16"); err != nil {
		panic(fmt.Sprintf("error parsing cidr 192.168.0.0/16: %s", err))
	}
}

type Logger interface {
	Printf(string, ...interface{})
}

type loggerWriter struct {
	logger Logger
}

func (lw *loggerWriter) Write(b []byte) (int, error) {
	if lw.logger == nil {
		return 0, nil
	}
	l := len(b)
	if l == 0 {
		return 0, nil
	}
	lw.logger.Printf(string(b))
	return l, nil
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
	envAddr := os.Getenv(EnvConsulLocalAddr)
	if envAddr != "" {
		return net.ParseIP(envAddr), nil
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	myInterface := os.Getenv(EnvConsulLocalInterface)

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

// SlugParams is used by the ReplaceSlugs helper function
type SlugParams struct {
	Name string
	Addr string
	Node string
}

// ReplaceSlugs does just that based on the provided input
func ReplaceSlugs(s string, p SlugParams) string {
	for i, cnt := 0, strings.Count(s, SlugRand); i < cnt; i++ {
		s = strings.Replace(s, SlugRand, LazyRandomString(12), 1)
	}
	s = strings.ReplaceAll(s, SlugName, p.Name)
	s = strings.ReplaceAll(s, SlugAddr, p.Addr)
	s = strings.ReplaceAll(s, SlugNode, p.Node)
	s = strings.ReplaceAll(s, SlugUnix, strconv.Itoa(int(time.Now().Unix())))
	return s
}

func simpleToReg(localAddr, localHostname string, reg *SimpleServiceRegistration) (string, *api.AgentServiceRegistration, error) {
	var (
		serviceID   string                 // local service identifier
		address     string                 // service host address
		interval    string                 // check renewInterval
		checkHTTP   *api.AgentServiceCheck // http type check
		serviceName string                 // service registration name
	)

	// Perform some basic service name cleanup and validation
	serviceName = strings.TrimSpace(reg.Name)
	if serviceName == "" {
		return "", nil, errors.New("\"Name\" cannot be blank")
	}
	if strings.Contains(serviceName, " ") {
		return "", nil, fmt.Errorf("name \"%s\" is invalid, service names cannot contain spaces", serviceName)
	}

	// Come on, guys...valid ports plz...
	if reg.Port <= 0 {
		return "", nil, fmt.Errorf("%d is not a valid port", reg.Port)
	}

	if address = reg.Address; address == "" {
		address = localAddr
	}

	if serviceID = reg.ID; serviceID == "" {
		// Form a unique service id
		var tail string
		if reg.RandomID {
			tail = LazyRandomString(12)
		} else {
			tail = strings.ToLower(localHostname)
		}
		serviceID = fmt.Sprintf("%s-%s", serviceName, tail)
	}

	if interval = reg.Interval; interval == "" {
		// set a default renewInterval
		interval = "30s"
	}

	// The serviceID is added in order to ensure detection in ServiceMonitor()
	tags := append(reg.Tags, serviceID)

	// Set up the service registration struct
	asr := &api.AgentServiceRegistration{
		ID:                serviceID,
		Name:              serviceName,
		Tags:              tags,
		Port:              reg.Port,
		Address:           address,
		Checks:            make(api.AgentServiceChecks, 0),
		EnableTagOverride: reg.EnableTagOverride,
	}

	// allow port override
	checkPort := reg.CheckPort
	if checkPort <= 0 {
		checkPort = reg.Port
	}

	// build http check if specified
	if reg.CheckPath != "" {

		// allow scheme override
		checkScheme := reg.CheckScheme
		if checkScheme == "" {
			checkScheme = "http"
		}

		// build check url
		checkURL := &url.URL{
			Scheme: checkScheme,
			Host:   fmt.Sprintf("%s:%d", address, checkPort),
			Path:   reg.CheckPath,
		}

		// build check
		checkHTTP = &api.AgentServiceCheck{
			HTTP:     checkURL.String(),
			Interval: interval,
		}

		// add http check
		asr.Checks = append(asr.Checks, checkHTTP)
	}

	// build tcp check if specified
	if reg.CheckTCP {
		// create tcp check definition
		asr.Checks = append(asr.Checks, &api.AgentServiceCheck{
			TCP:      fmt.Sprintf("%s:%d", address, checkPort),
			Interval: interval,
		})
	}

	return serviceID, asr, nil
}

// buildDefaultSessionName provides us with a usable session name if the user did not configure one themselves.
func buildDefaultSessionName(base *api.SessionEntry) string {
	var nodeName string
	if base.Node == "" {
		if hostName, herr := os.Hostname(); herr != nil {
			nodeName = SessionDefaultNodeName
		} else {
			nodeName = hostName
		}
	}
	return ReplaceSlugs(SessionDefaultNameFormat, SlugParams{Node: nodeName})
}
