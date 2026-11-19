package actuation

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// collectorRoutes are the only routes a collector token reaches
// (R5:5.5-12 §1.3, O7 A), by method and the full route pattern chi serves
// the request with. Every other route of /api/v1, in every module, answers
// a collector 403 before its own gate runs. What each route then holds the
// collector to is the route's own: its records only (agentrtapi), its host
// only and no title (agentthreads).
//
// server/testdata/collector_route_surface.txt classifies every route the
// platform serves; a route added without a line there fails
// TestCollectorRouteSurface.
var collectorRoutes = map[string]bool{
	"POST /api/v1/agentrt/external-work":             true,
	"POST /api/v1/agentrt/external-work/{id}/events": true,
	"PUT /api/v1/agentrt/artifacts/{id}":             true,
	"GET /api/v1/agentrt/whoami":                     true,
	"GET /api/v1/agentrt/external-work/{id}":         true,
	"GET /api/v1/agentrt/artifacts/{id}":             true,
	"POST /api/v1/agent/threads/heartbeat":           true,
	"POST /api/v1/agent/hosts/heartbeat":             true,
	"GET /api/v1/agent/threads":                      true,
}

// CollectorRoutes is a copy of the table, for the server to hand
// CollectorAllowed: nobody can change the table through it.
func CollectorRoutes() map[string]bool {
	out := make(map[string]bool, len(collectorRoutes))
	for route, allowed := range collectorRoutes {
		out[route] = allowed
	}
	return out
}

// CollectorAllowed says whether the request is one routes admits; the answer
// ConfineCollectors asks for.
// The route is the one chi will serve it with, found from the root router
// as chi routes it (the escaped path when the client escaped it, the method
// as sent), so no spelling of a path or method reaches another route than
// the one checked here. A request chi serves no route for is not admitted.
func CollectorAllowed(root chi.Routes, routes map[string]bool) func(*http.Request) bool {
	return func(r *http.Request) bool {
		path := r.URL.RawPath
		if path == "" {
			path = r.URL.Path
		}
		pattern := root.Find(chi.NewRouteContext(), r.Method, path)
		return pattern != "" && routes[r.Method+" "+pattern]
	}
}
