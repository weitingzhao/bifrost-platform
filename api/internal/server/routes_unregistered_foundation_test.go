package server

// Routes served before 2026-11-22 whose handler is in a foundation package and that
// have no contract row yet (routes_registered_test.go). The list only
// shrinks: a route leaves when its domain registers it.
var _ = heldUnregistered(
	"GET /api/v1/agentrt/artifacts",
	"GET /api/v1/agentrt/artifacts/{id}",
	"GET /api/v1/agentrt/events",
	"GET /api/v1/bus/states",
	"GET /api/v1/cluster",
	"GET /api/v1/cluster/events",
	"GET /api/v1/cluster/governance",
	"GET /api/v1/cluster/metrics",
	"GET /api/v1/cluster/namespaces",
	"GET /api/v1/cluster/nodes",
	"GET /api/v1/cluster/nodes/{name}/power",
	"GET /api/v1/cluster/observability",
	"GET /api/v1/cluster/placement",
	"GET /api/v1/cluster/postgres",
	"GET /api/v1/cluster/postgres/backup-status",
	"GET /api/v1/cluster/redis",
	"GET /api/v1/cluster/service-readiness",
	"GET /api/v1/cluster/workloads",
	"GET /api/v1/cluster/workloads/pods/{namespace}/{name}/logs",
	"GET /api/v1/delivery/prod-judgements",
	"GET /api/v1/network/clients/{mac}",
	"POST /api/v1/bus/hooks/gitea",
	"PUT /api/v1/agentrt/artifacts/{id}",
)
