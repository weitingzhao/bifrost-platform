package codeguard

// The foundation domain's test files that reach another domain (tests_reach_test.go).
// The list only shrinks.
var _ = heldAcross(map[string][]string{
	"api/internal/agentrt/migrate/helpers0022_test.go": {"api/internal/releasepolicy"},
	"api/internal/codeguard/anchored_scope_test.go":    {"api/internal/releasepolicy"},
})
