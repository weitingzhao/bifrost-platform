package server

// Routes served before 2026-11-22 whose handler is in a compute package and that
// have no contract row yet (routes_registered_test.go). The list only
// shrinks: a route leaves when its domain registers it.
var _ = heldUnregistered(
	"GET /api/v1/agent/bridge",
	"GET /api/v1/agent/deploy",
	"GET /api/v1/agent/launchd",
	"GET /api/v1/patrol/runs",
	"GET /api/v1/patrol/skills",
	"POST /api/v1/patrol/trigger/{id}",
	"POST /api/v1/patrol/webhook/{event}",
	"PUT /api/v1/patrol/skills/{id}/enable",
)
