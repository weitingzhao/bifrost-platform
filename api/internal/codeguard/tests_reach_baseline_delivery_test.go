package codeguard

// The delivery domain's test files that reach another domain (tests_reach_test.go).
// The list only shrinks.
var _ = heldAcross(map[string][]string{
	"api/internal/delivery/pipeline_steps_results_test.go": {"api/internal/cirunview", "api/internal/unitexec"},
	"api/internal/delivery/run_refusal_test.go":            {"api/internal/placement"},
})
