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
		"tuning validity":             {tuning, loosened(t, tuning, "2027-01-15T00:00:00Z", "2027-02-15T00:00:00Z"), false},
		"per_host up":                 {concurrency, loosened(t, concurrency, "per_host: 1", "per_host: 3"), true},
		"per_host down":               {concurrency, loosened(t, concurrency, "per_host: 1", "per_host: 0"), true},
		"nothing changes":             {concurrency, loosened(t, concurrency, "", ""), false},
		"expiring later":              {concurrency, loosened(t, concurrency, "2027-01-15T00:00:00Z", "2027-02-15T00:00:00Z"), false},
		"budget mission up":           {budget, loosened(t, budget, "max_attempts: 40", "max_attempts: 80"), true},
		"budget hard limit up":        {budget, loosened(t, budget, "attempt_hard_limit_s: 3600", "attempt_hard_limit_s: 7200"), true},
		"budget states auto_grant":    {budget, loosened(t, budget, "attempt_hard_limit_s: 3600\n", "attempt_hard_limit_s: 3600\nauto_grant: {max_attempts: 60, max_wall_hours: 24}\n"), true},
		"routing quota up":            {routing, loosened(t, routing, "quota_stop_percent: 90", "quota_stop_percent: 95"), true},
		"routing competence":          {routing, loosened(t, routing, "quota_stop_percent: 90\n", "quota_stop_percent: 90\nmin_score_percent: 50\nmin_samples: 3\n"), false},
		"intake max live up":          {intake, loosened(t, intake, "max_live_per_product: 1", "max_live_per_product: 4"), true},
		"intake drops an exception":   {intake, loosened(t, intake, "[fault_small, answer_only]", "[answer_only]"), false},
		"landing limits up":           {capacityLanding, loosened(t, capacityLanding, "max_landings_per_day: 5", "max_landings_per_day: 9"), true},
		"landing repo limit up":       {capacityLanding, loosened(t, capacityLanding, "max_files: 8", "max_files: 12"), true},
		"landing global in flight up": {capacityLanding, loosened(t, capacityLanding, "max_in_flight_per_repo: 1", "max_in_flight_per_repo: 2"), true},
		"landing repo in flight up":   {capacityLanding, loosened(t, capacityLanding, "max_in_flight: 1}", "max_in_flight: 2}"), true},
		"landing allow grows":         {capacityLanding, loosened(t, capacityLanding, `["console/src/pages/**/*.tsx"]`, `["console/src/pages/**/*.tsx", "api/**"]`), false},
		"an envelope's budget":        {envelope, loosened(t, envelope, "max_attempts: 40", "max_attempts: 80"), false},
		"a job":                       {job, loosened(t, job, "status: probation", "status: active"), false},
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
	next = parse(t, loosened(t, routing, "quota_stop_percent: 90\n", "quota_stop_percent: 95\nmin_score_percent: 50\nmin_samples: 3\n"))
	err = rules.CapacityOnly(parse(t, routing), next)
	if err == nil || !strings.Contains(err.Error(), "min_samples") || !strings.Contains(err.Error(), "min_score_percent") {
		t.Fatalf("CapacityOnly = %v, want the competence fields named", err)
	}
	if err = rules.CapacityOnly(parse(t, concurrency), parse(t, budget)); err == nil {
		t.Fatal("two different rules compare")
	}
}
