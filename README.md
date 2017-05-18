# consultant
Helpful wrappers around Consul API client

[![](https://img.shields.io/badge/godoc-reference-5272B4.svg?style=flat-square)](https://godoc.org/github.com/myENA/consultant)
[![Build Status](https://travis-ci.org/myENA/consultant.svg?branch=master)](https://travis-ci.org/myENA/consultant)

## Client
Our Consultant [Client](./client.go#L14) is a very thin wrapper around the 
[Consul API Client](https://github.com/hashicorp/consul/blob/v0.8.2/api/api.go#L356).  It provides

- Simplified Service Retrieval ([see here](./client.go#L51))
- Simplified Service Registration ([see here](./client.go#L83))

## Sibling Services Locator
If you run in an environment where you are running several instances of the same service, it can be useful sometimes
to have a way for each service too find it's sibling services.

Look [here](./sibling_locator.go#L61) for some basic documentation.

## Service Candidate Election
With a multi-service setup, there are times where might want one service to be responsible for a specific task.
This task can range from being considered the leader of the entire cluster of services OR simply a single sub-task
that must run atomically.

Look [here](./candidate.go#L53) for some basic documentation.

## Managed Services
[ManagedService](./managed_service.go) is a lightweight lifecycle manager for Consul services.

## Watch Plan Helpers
[watch.go](./watch.go) contains two sets of methods:

- Package helper functions that will construct one of the 
  [already available](https://github.com/hashicorp/consul/blob/master/watch/funcs.go#L17) watch functions
  provided by hashi
- Helper methods hanging off of our [Client](./client.go) struct that will fill in some values based on the client's
  own definition

## TODO:
- More tests
- More docs
- More stuff
- ManagedService consistency checks
