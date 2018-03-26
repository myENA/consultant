package service

import (
	"errors"
	"fmt"
	consulapi "github.com/hashicorp/consul/api"
	"github.com/myENA/consultant/util"
	"net/url"
	"strings"
	"time"
)

// RegisterSimple will attempt to register a service and health check with Consul, optionally using a provided client.
// If no client is provided one will be created using the default configuration
//
// Returns the ID of the registered service or whatever error is seen.
func RegisterSimple(registration *SimpleRegistration, client *consulapi.Client) (string, error) {
	if registration == nil {
		return "", errors.New("registration cannot be nil")
	}

	var err error

	client = apiClient(client)

	serviceName := strings.TrimSpace(registration.Name)
	if serviceName == "" {
		return "", errors.New("service name cannot be empty")
	}

	if registration.Port <= 0 {
		return "", errors.New("port must be > 0")
	}

	address := registration.Address
	if address == "" {
		address, err = util.MyAddress()
		if err != nil {
			return "", fmt.Errorf("no address provided and unable to determine one: %s", err)
		}
	}

	serviceID := registration.ID
	if serviceID == "" {
		serviceID, err = GenerateID(serviceName, registration.RandomID)
		if err != nil {
			return "", err
		}
	}

	interval := registration.CheckInterval
	if interval == "" {
		interval = "30s"
	}

	_, err = time.ParseDuration(interval)
	if err != nil {
		return "", fmt.Errorf("\"%s\" is not a valid duration: %s", interval, err)
	}

	serviceTags := append(registration.Tags, serviceID)

	asr := &consulapi.AgentServiceRegistration{
		ID:                serviceID,
		Name:              serviceName,
		Tags:              serviceTags,
		Address:           address,
		Port:              registration.Port,
		Checks:            make(consulapi.AgentServiceChecks, 0),
		EnableTagOverride: registration.EnableTagOverride,
	}

	checkPort := registration.CheckPort
	if checkPort <= 0 {
		checkPort = registration.Port
	}

	if registration.CheckPath != "" {
		checkScheme := registration.CheckScheme
		if checkScheme == "" {
			checkScheme = "http"
		}

		checkUrl := &url.URL{
			Scheme: checkScheme,
			Host:   fmt.Sprintf("%s:%d", address, checkPort),
			Path:   registration.CheckPath,
		}

		checkHTTP := &consulapi.AgentServiceCheck{
			HTTP:     checkUrl.String(),
			Interval: interval,
		}

		asr.Checks = append(asr.Checks, checkHTTP)
	}

	if registration.CheckTCP {
		asr.Checks = append(asr.Checks, &consulapi.AgentServiceCheck{
			TCP:      fmt.Sprintf("%s:%d", address, checkPort),
			Interval: interval,
		})
	}

	if err = client.Agent().ServiceRegister(asr); err != nil {
		return "", fmt.Errorf("unable to register serivice: %s", err)
	}

	return serviceID, nil
}
