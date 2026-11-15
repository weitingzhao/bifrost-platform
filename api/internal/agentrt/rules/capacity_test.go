package rules_test

import (
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

var capacityLanding = text("landing:dev", `tenant: dev
repos:
  - repo: bifrost-platform
    allow: ["console/src/pages/**/*.tsx"]
    deny: []
    stg_release: none
    limits: {max_files: 8, max_changed_lines: 200, max_in_flight: 1}
limits:
  max_files: 10
  max_changed_lines: 300
  max_landings_per_day: 5
  max_in_flight_per_repo: 1
`)

// The next version of base, marked loosen, with from replaced by to.
func loosened(t *testing.T, base, from, to string) string {
	t.Helper()
	s := strings.Replace(base, "rule_version: 1\ndirection: initial", "rule_version: 2\ndirection: loosen", 1)
	if from != "" {
		if !strings.Contains(s, from) {
			t.Fatalf("no %q in\n%s", from, s)
		}
		s = strings.Replace(s, from, to, 1)
	}
	return s
}

// A version that changes only capacity fields, within their ceilings, goes
// on a card; one that changes anything else, nothing, or a kind with no
// capacity fields does not (Q5 A).
func TestCapacityOnly(t *testing.T) {
	for name, c := range map[string]struct {
		prev, next string
		card       bool
	}{
		"tuning capacity":             {tuning, loosened(t, tuning, "assess_per_pass: 5", "assess_per_pass: 6"), true},
		"nothing changes":             {tuning, loosened(t, tuning, "", ""), false},
		"tuning validity":             {tuning, loosened(t, tuning, "2027-01-15T00:00:00Z", "2027-02-15T00:00:00Z"), false},
		"landing limits up":           {capacityLanding, loosened(t, capacityLanding, "max_landings_per_day: 5", "max_landings_per_day: 9"), true},
		"landing repo limit up":       {capacityLanding, loosened(t, capacityLanding, "max_files: 8", "max_files: 12"), true},
		"landing global in flight up": {capacityLanding, loosened(t, capacityLanding, "max_in_flight_per_repo: 1", "max_in_flight_per_repo: 2"), true},
		"landing repo in flight up":   {capacityLanding, loosened(t, capacityLanding, "max_in_flight: 1}", "max_in_flight: 2}"), true},
		// max_batch is legacy (cleanup wave 1): no longer a signed field.
		"landing states max_batch":  {capacityLanding, loosened(t, capacityLanding, "max_in_flight_per_repo: 1\n", "max_in_flight_per_repo: 1\n  max_batch: 24\n"), true},
		"landing changes max_batch": {loosened(t, capacityLanding, "max_in_flight_per_repo: 1\n", "max_in_flight_per_repo: 1\n  max_batch: 24\n"), strings.Replace(loosened(t, capacityLanding, "max_in_flight_per_repo: 1\n", "max_in_flight_per_repo: 1\n  max_batch: 2000\n"), "rule_version: 2", "rule_version: 3", 1), true},
		"landing drops max_batch":   {loosened(t, capacityLanding, "max_in_flight_per_repo: 1\n", "max_in_flight_per_repo: 1\n  max_batch: 24\n"), strings.Replace(loosened(t, capacityLanding, "", ""), "rule_version: 2", "rule_version: 3", 1), true},
		"landing allow grows":       {capacityLanding, loosened(t, capacityLanding, `["console/src/pages/**/*.tsx"]`, `["console/src/pages/**/*.tsx", "api/**"]`), false},
	} {
		err := rules.CapacityOnly(parse(t, c.prev), parse(t, c.next))
		if (err == nil) != c.card {
			t.Errorf("%s: CapacityOnly = %v, want card %v", name, err, c.card)
		}
	}
}

// The changes a card shows: each capacity field with its old and new value;
// the others named, so the refusal says what needs a signature.
func TestCapacityOnlyChanges(t *testing.T) {
	prev := parse(t, capacityLanding)
	next := parse(t, loosened(t, capacityLanding, "max_files: 8", "max_files: 12"))
	changes, others, err := rules.CapacityChanges(prev, next)
	if err != nil || len(others) != 0 {
		t.Fatalf("CapacityChanges = %v, %v", others, err)
	}
	if len(changes) != 1 || changes[0].Field != "repos[bifrost-platform].limits.max_files" || changes[0].Old != 8 || changes[0].New != 12 {
		t.Fatalf("changes = %+v", changes)
	}
	if err = rules.CapacityOnly(parse(t, tuning), parse(t, capacityLanding)); err == nil {
		t.Fatal("two different rules compare")
	}
}
