package codeguard

// The plan domain's test files that reach another domain (tests_reach_test.go).
// The list only shrinks.
var _ = heldAcross(map[string][]string{
	"api/internal/agentrt/planstore/planobservations_ledger_test.go":     {"api/internal/agentrt/planwiring"},
	"api/internal/agentrt/planstore/planobservations_regression_test.go": {"api/internal/agentrt/landing"},
	"api/internal/agentrt/planstore/planviewcollector_fixture_test.go":   {"api/internal/agentrt/externalwork"},
})
