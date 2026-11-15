package rules

import "strings"

// The rule kinds retired with the dispatch lane (the Owner's coordinator,
// 2026-11-15; side-session notes biz-align/rule-kinds-retirement-2026-11-15.md):
// measured that day, no route and no loop decided anything by them any more.
// Their signed versions stay in agentrt.rule, which is append-only; nothing
// parses them. A reader that walks every rule asks Retired first.
var retiredKinds = []string{"standing-envelope", "concurrency", "budget", "job", "capability-facts", "routing", "landing", "intake-gates", "debt-projects"}

// Retired says whether a rule id is of a retired kind: the kind itself, or
// the kind and what it is for ("job:land", "budget:dev").
func Retired(id string) bool {
	kind, _, _ := strings.Cut(id, ":")
	for _, k := range retiredKinds {
		if kind == k {
			return true
		}
	}
	return false
}
