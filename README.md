# consultant
Helpful wrappers around Consul API client

[![](https://img.shields.io/badge/godoc-reference-5272B4.svg?style=flat-square)](https://godoc.org/github.com/myENA/consultant)
[![Build Status](https://travis-ci.org/myENA/consultant.svg?branch=master)](https://travis-ci.org/myENA/consultant)

## Client
Our Consultant <a href="https://godoc.org/github.com/myENA/consultant#Client" target="_blank">Client</a> is a wrapper around the
<a href="https://github.com/hashicorp/consul/blob/v1.6.2/api/api.go" target ="_blank">Consul API Client</a>.  It provides

- Simplified Service Retrieval (<a href="https://godoc.org/github.com/myENA/consultant#Client.PickService" target="_blank">docs</a>)
- Simplified Service Registration (<a href="https://godoc.org/github.com/myENA/consultant#Client.SimpleServiceRegister" target="_blank">docs</a>)
- Simplified Key Retrieval (<a href="https://godoc.org/github.com/myENA/consultant#Client.EnsureKey" target="_blank">docs</a>)


## Managed Types
This package also provides a few "managed" types, complete with a <a href="https://godoc.org/github.com/myENA/consultant#Notifier" _target="blank">Notification system</a>.

- <a href="https://godoc.org/github.com/myENA/consultant#ManagedService" _target="blank">ManagedService</a>
- <a href="https://godoc.org/github.com/myENA/consultant#ManagedSession" _target="blank">ManagedSession</a>
- <a href="https://godoc.org/github.com/myENA/consultant#Candidate" _target="blank">Candidate</a>

## Watch Plan Helpers
[watch.go](watch.go) contains two sets of methods:

- Package helper functions that will construct one of the <a href="https://github.com/hashicorp/consul/blob/master/watch/funcs.go#L17" target="_blank">already available</a> watch functions provided by the consul watch package
- Helper methods hanging off of our <a href="https://godoc.org/github.com/myENA/consultant#Client" _target="blank">Client</a> struct that will fill in some values based on the client's own definition

## TODO:
- More tests
- More docs
- More stuff
- ManagedService consistency checks
