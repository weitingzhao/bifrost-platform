package rules_test

import (
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

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
		"tuning capacity": {tuning, loosened(t, tuning, "assess_per_pass: 5", "assess_per_pass: 6"), true},
		"nothing changes": {tuning, loosened(t, tuning, "", ""), false},
		"tuning validity": {tuning, loosened(t, tuning, "2027-01-15T00:00:00Z", "2027-02-15T00:00:00Z"), false},
		// max_batch is legacy (cleanup wave 1): no longer a signed field.
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
	prev := parse(t, tuning)
	next := parse(t, loosened(t, tuning, "assess_per_pass: 5", "assess_per_pass: 6"))
	changes, others, err := rules.CapacityChanges(prev, next)
	if err != nil || len(others) != 0 {
		t.Fatalf("CapacityChanges = %v, %v", others, err)
	}
	if len(changes) != 1 || changes[0].Field != "values.assess_per_pass" || changes[0].Old != 5 || changes[0].New != 6 {
		t.Fatalf("changes = %+v", changes)
	}
}
