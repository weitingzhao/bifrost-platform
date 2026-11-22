package codeguard

// The approvals domain's test files that reach another domain (tests_reach_test.go).
// The list only shrinks.
var _ = heldAcross(map[string][]string{
	"api/internal/actions/coverage_test.go":                              {"api/internal/server"},
	"api/internal/actions/ownerrun_d10_test.go":                          {"api/internal/releasepolicy"},
	"api/internal/approvals/http_test.go":                                {"api/internal/server"},
	"api/internal/approvals/thread_inbox_legacy_test.go":                 {"api/internal/threadinbox"},
	"api/internal/approvals/thread_inbox_order_pg_test.go":               {"api/internal/agentrt/coord"},
	"api/internal/approvals/thread_inbox_sources_pg_test.go":             {"api/internal/agentrt/coord"},
	"api/internal/approvals/thread_inbox_test.go":                        {"api/internal/agentrt/coord"},
	"api/internal/assessmentreadings/assessment_readings_parity_test.go": {"api/internal/agentrt/planstore"},
	"api/internal/questions/thread_inbox_legacy_test.go":                 {"api/internal/threadinbox"},
	"api/internal/questions/thread_inbox_pg_test.go":                     {"api/internal/agentrt/coord"},
})
