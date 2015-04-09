// Package cluster implements a client for the Flynn host service.
package cluster

import (
	"errors"
	"net/http"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/dialer"
)

// NewClient uses the default discoverd configuration to create a cluster
// client.
func NewClient() *Client {
	return NewClientWithServices(nil)
}

// A ServiceFunc is a function that takes a service name and returns
// a discoverd.Service.
type ServiceFunc func(name string) discoverd.Service

// NewClientWithServices uses the provided services to find cluster members. If
// services is nil, the default discoverd client is used.
func NewClientWithServices(services ServiceFunc) *Client {
	hc := &http.Client{Transport: &http.Transport{Dial: dialer.Retry.Dial}}
	return NewClientWithHTTP(services, hc)
}

func NewClientWithHTTP(services ServiceFunc, hc *http.Client) *Client {
	return newClient(services, hc)
}

// ErrNotFound is returned when a resource is not found (HTTP status 404).
var ErrNotFound = errors.New("cluster: resource not found")

func newClient(services ServiceFunc, hc *http.Client) *Client {
	if services == nil {
		services = discoverd.NewService
	}
	s := services("flynn-host")
	return &Client{s: s, h: hc}
}

// A Client is used to discover members of the flynn-host cluster.
type Client struct {
	s discoverd.Service
	h *http.Client
}

// Hosts returns a list of hosts in the cluster.
func (c *Client) Hosts() ([]Host, error) {
	insts, err := c.s.Instances()
	if err != nil {
		return nil, err
	}
	hosts := make([]Host, len(insts))
	for i, inst := range insts {
		hosts[i] = NewHostClient(inst.ID, inst.Addr, c.h)
	}
	return hosts, nil
}
