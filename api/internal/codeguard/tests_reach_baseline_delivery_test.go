package codeguard

// The delivery domain's test files that reach another domain (tests_reach_test.go).
// The list only shrinks.
var _ = heldAcross(map[string][]string{
	"api/internal/delivery/ci_reader_test.go":              {"api/internal/actions"},
	"api/internal/delivery/pipeline_steps_results_test.go": {"api/internal/cirunview", "api/internal/unitexec"},
	"api/internal/delivery/run_refusal_test.go":            {"api/internal/placement"},
	"api/internal/releasepolicy/apply_test.go":             {"api/internal/actions"},
	"api/internal/releasepolicy/ddl_test.go":               {"api/internal/actions"},
	"api/internal/releasepolicy/migration_test.go":         {"api/internal/actions"},
	"api/internal/releasepolicy/mustrefuse/cases_test.go":  {"api/internal/actions"},
	"api/internal/releasepolicy/mustrefuse/world_test.go":  {"api/internal/actions"},
	"api/internal/releasepolicy/releasepolicy_test.go":     {"api/internal/actions"},
	"api/internal/releasepolicy/snapshot_test.go":          {"api/internal/actions"},
})
