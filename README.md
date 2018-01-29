# consultant
Helpful wrappers around Consul API client

[![](https://img.shields.io/badge/godoc-reference-5272B4.svg?style=flat-square)](https://godoc.org/github.com/myENA/consultant)
[![Build Status](https://travis-ci.org/myENA/consultant.svg?branch=master)](https://travis-ci.org/myENA/consultant)

## Client
Our Consultant [![Client]](https://godoc.org/github.com/myENA/consultant#Client) is a thin wrapper around the
[![Consul API Client]](https://github.com/hashicorp/consul/blob/v1.0.3/api/api.go).  It provides

- Simplified Service Retrieval ([![see here]](https://godoc.org/github.com/myENA/consultant#Client.PickService))
- Simplified Service Registration ([![see here]](https://godoc.org/github.com/myENA/consultant#Client.SimpleServiceRegister))
- Simplified Key Retrieval ([![see here]](https://godoc.org/github.com/myENA/consultant#Client.EnsureKey))

## Sibling Services Locator
If you run in an environment where you are running several instances of the same service, it can be useful sometimes
to have a way for each service too find it's sibling services.

Look [![here]](https://godoc.org/github.com/myENA/consultant#SiblingLocator) for some basic documentation.

## Service Candidate Election
With a multi-service setup, there are times where might want one service to be responsible for a specific task.
This task can range from being considered the leader of the entire cluster of services OR simply a single sub-task
that must run atomically.

Look [![here]](https://godoc.org/github.com/myENA/consultant#Candidate) for some basic documentation.

## Managed Services
[![ManagedService]](https://godoc.org/github.com/myENA/consultant#ManagedService) is a lightweight lifecycle manager for Consul services.

## Watch Plan Helpers
[watch.go](watch.go) contains two sets of methods:

- Package helper functions that will construct one of the 
  [![already available]](https://github.com/hashicorp/consul/blob/master/watch/funcs.go#L17) watch functions
  provided by hashi
- Helper methods hanging off of our [Client](./client.go) struct that will fill in some values based on the client's
  own definition

## TODO:
- More tests
- More docs
- More stuff
- ManagedService consistency checks
