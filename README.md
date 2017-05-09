# consultant
Helpful wrappers around Consul API client

[![](https://img.shields.io/badge/godoc-reference-5272B4.svg?style=flat-square)](https://godoc.org/github.com/myENA/consultant)
[![Build Status](https://travis-ci.org/myENA/consultant.svg?branch=master)](https://travis-ci.org/myENA/consultant)

## Sibling Services Locator
If you run in an environment where you are running several instances of the same service, it can be useful sometimes
to have a way for each service too find it's sibling services.

Look [here](./sibling_locator.go#L61) for some basic documentation.

## Service Candidate Election
With a multi-service setup, there are times where might want one service to be responsible for a specific task.
This task can range from being considered the leader of the entire cluster of services OR simply a single sub-task
that must run atomically.

Look [here](./candidate.go#L53) for some basic documentation.

## Client
Our Consultant [Client](./client.go#L14) is a very thin wrapper around the 
[Consul API Client](https://github.com/hashicorp/consul/blob/v0.8.2/api/api.go#L356).  It provides

- Simplified Service Retrieval ([see here](./client.go#L51))
- Simplified Service Registration ([see here](./client.go#L83))

## TODO:
- More tests
- More docs
- More stuff
