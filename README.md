# consultant
Helpful wrappers around Consul API client

[![](https://img.shields.io/badge/godoc-reference-5272B4.svg?style=flat-square)](https://godoc.org/github.com/myENA/consultant)
[![Build Status](https://travis-ci.org/myENA/consultant.svg?branch=master)](https://travis-ci.org/myENA/consultant)

## Client
Our Consultant <a href="https://godoc.org/github.com/myENA/consultant#Client" target="_blank">Client</a> is a thin wrapper around the
<a href="https://github.com/hashicorp/consul/blob/v1.0.3/api/api.go" target ="_blank">Consul API Client</a>.  It provides

- Simplified Service Retrieval (<a href="https://godoc.org/github.com/myENA/consultant#Client.PickService" target="_blank">docs</a>)
- Simplified Service Registration (<a href="https://godoc.org/github.com/myENA/consultant#Client.SimpleServiceRegister" target="_blank">docs</a>)
- Simplified Key Retrieval (<a href="https://godoc.org/github.com/myENA/consultant#Client.EnsureKey" target="_blank">docs</a>)

## <a href="https://godoc.org/github.com/myENA/consultant#Candidate" target="_blank">Service Candidate Election</a>
With a multi-service setup, there are times where might want one service to be responsible for a specific task.
This task can range from being considered the leader of the entire cluster of services OR simply a single sub-task
that must run atomically.

## <a href="https://godoc.org/github.com/myENA/consultant#ManagedService" target="_blank">Managed Services</a>
Service lifecycle manager

## Watch Plan Helpers
[watch.go](watch.go) contains two sets of methods:

- Package helper functions that will construct one of the 
  <a href="https://github.com/hashicorp/consul/blob/master/watch/funcs.go#L17" target="_blank">already available</a> watch functions
  provided by hashi
- Helper methods hanging off of our [Client](./client.go) struct that will fill in some values based on the client's
  own definition

## TODO:
- More tests
- More docs
- More stuff
- ManagedService consistency checks
