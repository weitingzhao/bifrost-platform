package codeguard

// The baselines of tests_reach_test.go, 2026-11-17 (platform origin/main
// 9ca1d391): 62 test files in a package of one domain that import 63
// packages of other domains or shared ones, and 278 test files in packages
// several domains share. Both only shrink. The 30 plan test files left in
// agentrt/store are TD-582 (infra agent-config/TECH_DEBT.md; the decoupling
// line's notes biz-align/planstore-tests-2026-11-17.md). 2026-11-18: the
// cross-domain ones moved to agentrt/integration, their paths with them.
// 2026-11-20: agentrt/agentrtapi folded into server, its paths with it
// (its outside_inputs_test.go merged into server's).

var testsReachingAcross = map[string][]string{
	"api/internal/actions/coverage_test.go":                              {"api/internal/server"},
	"api/internal/actions/ownerrun_d10_test.go":                          {"api/internal/releasepolicy"},
	"api/internal/agentrt/migrate/helpers0022_test.go":                   {"api/internal/releasepolicy"},
	"api/internal/agentrt/planstore/planobservations_ledger_test.go":     {"api/internal/agentrt/planwiring"},
	"api/internal/agentrt/planstore/planobservations_regression_test.go": {"api/internal/agentrt/landing"},
	"api/internal/agentrt/planstore/planviewcollector_fixture_test.go":   {"api/internal/agentrt/externalwork"},
	"api/internal/approvals/http_test.go":                                {"api/internal/server"},
	"api/internal/approvals/thread_inbox_legacy_test.go":                 {"api/internal/threadinbox"},
	"api/internal/approvals/thread_inbox_order_pg_test.go":               {"api/internal/agentrt/coord"},
	"api/internal/approvals/thread_inbox_sources_pg_test.go":             {"api/internal/agentrt/coord"},
	"api/internal/approvals/thread_inbox_test.go":                        {"api/internal/agentrt/coord"},
	"api/internal/assessmentreadings/assessment_readings_parity_test.go": {"api/internal/agentrt/planstore"},
	"api/internal/codeguard/anchored_scope_test.go":                      {"api/internal/releasepolicy"},
	"api/internal/delivery/approved_waits_test.go":                       {"api/internal/actions"},
	"api/internal/delivery/ci_reader_test.go":                            {"api/internal/actions"},
	"api/internal/delivery/pipeline_steps_results_test.go":               {"api/internal/cirunview", "api/internal/unitexec"},
	"api/internal/delivery/run_refusal_test.go":                          {"api/internal/actions", "api/internal/placement"},
	"api/internal/delivery/run_window_test.go":                           {"api/internal/actions"},
	"api/internal/delivery/run_window_workers_test.go":                   {"api/internal/actions"},
	"api/internal/questions/thread_inbox_legacy_test.go":                 {"api/internal/threadinbox"},
	"api/internal/questions/thread_inbox_pg_test.go":                     {"api/internal/agentrt/coord"},
	"api/internal/releasepolicy/apply_test.go":                           {"api/internal/actions"},
	"api/internal/releasepolicy/ddl_test.go":                             {"api/internal/actions"},
	"api/internal/releasepolicy/migration_test.go":                       {"api/internal/actions"},
	"api/internal/releasepolicy/mustrefuse/cases_test.go":                {"api/internal/actions"},
	"api/internal/releasepolicy/mustrefuse/world_test.go":                {"api/internal/actions"},
	"api/internal/releasepolicy/releasepolicy_test.go":                   {"api/internal/actions"},
	"api/internal/releasepolicy/snapshot_test.go":                        {"api/internal/actions"},
}

// 2026-11-21, law 71: all 115 were in server and integration, which became
// the assembly domain; its tests reach every domain as its code does. The
// list stays, empty: a package several domains share gets no test file.
var testFilesInShared = []string{}
