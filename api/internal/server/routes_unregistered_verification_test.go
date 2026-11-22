package server

// Routes served before 2026-11-22 whose handler is in a verification package and that
// have no contract row yet (routes_registered_test.go). The list only
// shrinks: a route leaves when its domain registers it.
var _ = heldUnregistered(
	"GET /api/v1/agentrt/rules/{rule_id}",
	"POST /api/v1/agentrt/rules/{rule_id}/check",
	"POST /api/v1/agentrt/rules/{rule_id}/proposals",
	"POST /api/v1/agentrt/rules/{rule_id}/sign",
	"POST /api/v1/agentrt/rules/{rule_id}/tighten",
)
