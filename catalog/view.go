package catalog

import (
	"sort"

	"github.com/Nitro/memberlist"
	"github.com/Nitro/sidecar/service"
)

// These are functions useful in viewing the contents of the state

// ServicesState -------------------------

func (state *ServicesState) EachServiceSorted(fn func(hostname *string, serviceId *string, svc *service.Service)) {
	var services []*service.Service
	state.EachService(func(hostname *string, serviceId *string, svc *service.Service) {
		services = append(services, svc)
	})

	sort.Sort(ServicesByAge(services))

	for _, svc := range services {
		fn(&svc.Hostname, &svc.ID, svc)
	}
}

func (state *ServicesState) EachLocalService(fn func(hostname *string, serviceId *string, svc *service.Service)) {
	state.EachService(func(hostname *string, serviceId *string, svc *service.Service) {
		if state.Hostname == *hostname {
			fn(hostname, serviceId, svc)
		}
	})
}

// Services -------------------------------
//   by Age
type ServicesByAge []*service.Service

func (s ServicesByAge) Len() int           { return len(s) }
func (s ServicesByAge) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s ServicesByAge) Less(i, j int) bool { return s[i].Updated.Before(s[j].Updated) }

func (s *Server) SortedServices() []*service.Service {
	servicesList := make([]*service.Service, 0, len(s.Services))

	for _, service := range s.Services {
		servicesList = append(servicesList, service)
	}

	sort.Sort(ServicesByAge(servicesList))

	return servicesList
}

//   by Name
type ServicesByName []*service.Service

func (a ServicesByName) Len() int           { return len(a) }
func (a ServicesByName) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ServicesByName) Less(i, j int) bool { return a[i].Name < a[j].Name }

// Servers --------------------------------
type ServerByName []*Server

func (s ServerByName) Len() int {
	return len(s)
}

func (s ServerByName) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s ServerByName) Less(i, j int) bool {
	return s[i].Name < s[j].Name
}

func (state *ServicesState) SortedServers() []*Server {
	serversList := make([]*Server, 0, len(state.Servers))

	for _, server := range state.Servers {
		serversList = append(serversList, server)
	}

	sort.Sort(ServerByName(serversList))

	return serversList
}

// Memberlist --------------------------------
//   by Name
type ListByName []*memberlist.Node

func (a ListByName) Len() int           { return len(a) }
func (a ListByName) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ListByName) Less(i, j int) bool { return a[i].Name < a[j].Name }
