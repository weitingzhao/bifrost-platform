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

// testsReachingAcross is filled per domain, one file each
// (tests_reach_baseline_<domain>_test.go), so two lines cutting their own
// entries do not edit the same lines. Origin: 2026-11-22, the CI line
// replayed the landings refused that day: 4 of 10 merge conflicts were in
// this file, every one between entries of different domains.
var testsReachingAcross = map[string][]string{}

// heldAcross adds one domain's entries to testsReachingAcross.
func heldAcross(entries map[string][]string) bool {
	for f, deps := range entries {
		testsReachingAcross[f] = deps
	}
	return true
}

// 2026-11-21, LAW-10: all 115 were in server and integration, which became
// the assembly domain; its tests reach every domain as its code does. The
// list stays, empty: a package several domains share gets no test file.
var testFilesInShared = []string{}
