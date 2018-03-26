package service

type (
	SimpleRegistration struct {
		// Name [required]
		//
		// This will be the "name" your service will appear as within Consul.
		Name string

		// Port [required]
		//
		// Should be the port your service listens on.
		Port int

		// ID [optional] - Mutually exclusive with RandomID
		//
		// Set if you wish for this service to have a specific ID.  If not set, an ID will be generated.
		ID string

		// RandomID [optional] - Mutually exclusive with ID
		//
		// If a specific ID is not provided, this determines the structure of the generated ID.  The structure is:
		//
		// 		"Name-{tail}"
		//
		// The value of "{tail}" depends on this value.  If set to "true", a 12 character pseudo-random tail will be
		// generated.  If "false", we will attempt to use the system hostname value provided by os.HostName()
		RandomID bool

		// Address [optional] - Defaults to value from util.MyAddress
		//
		// This should be the address your service will respond to.
		Address string

		// EnableTagOverride [optional]
		//
		// If using Consul 0.6+, setting this to "true" will allow you to update the tags for the service after
		// registration.
		EnableTagOverride bool

		// Tags [optional]
		//
		// Allows specifying tags to be added to service at time of registration.  We always append the ID of the
		// service to the list of tags.
		Tags []string

		// Health Check registration
		//
		// For more information on how Consul Health Checks work, see https://www.consul.io/docs/agent/checks.html

		// CheckPort [optional] - Defaults to Port value
		//
		// Used when creating the AgentServiceCheck(s) that will be registered with this service.  If left blank,
		// defaults to value provided to Port
		CheckPort int

		// Interval [optional] - Defaults to 30s
		//
		// Will be set as the check interval for the created AgentServiceCheck(s).
		CheckInterval string

		// CheckTCP [optional]
		//
		// Set to true to add a TCP check to this service
		CheckTCP bool

		// CheckPath [optional]
		//
		// If defined, a HTTP check will be created using this path
		CheckPath string

		// CheckScheme [optional] - Defaults to "http"
		//
		// The HTTP scheme to use with the HTTP check created when CheckPath is set
		CheckScheme string
	}
)
