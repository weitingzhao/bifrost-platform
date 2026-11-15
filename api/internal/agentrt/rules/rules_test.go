package rules_test

import (
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

const head = `rule_id: %s
rule_version: 1
direction: initial
effective_at: 2026-10-15T00:00:00Z
expires_at: 2027-01-15T00:00:00Z
`

func text(id, body string) string { return strings.Replace(head, "%s", id, 1) + body }

func parse(t *testing.T, s string) rules.Rule {
	t.Helper()
	r, err := rules.Parse([]byte(s))
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, s)
	}
	return r
}

// What the closed parser refuses (T35: a field it does not know, and every
// way YAML can make one text mean two things).
func TestParseRefuses(t *testing.T) {
	for name, s := range map[string]string{
		"an unknown field":          tuning + "note: hello\n",
		"an unknown nested field":   strings.Replace(tuning, "  assess_per_pass: 5", "  unknown: 5", 1),
		"an anchor":                 strings.Replace(tuning, "values:", "values: &v", 1),
		"an alias":                  strings.Replace(tuning, "values:", "values: &v", 1) + "other: *v\n",
		"a merge key":               strings.Replace(tuning, "  assess_per_pass: 5", "  <<: {assess_per_pass: 5}", 1),
		"an explicit tag":           strings.Replace(tuning, "assess_per_pass: 5", "assess_per_pass: !!int 5", 1),
		"a second document":         tuning + "---\nassess_per_pass: 5\n",
		"a key given twice":         tuning + "  assess_per_pass: 4\n",
		"a number as a string":      strings.Replace(tuning, "assess_per_pass: 5", `assess_per_pass: "5"`, 1),
		"a number not in decimal":   strings.Replace(tuning, "assess_per_pass: 5", "assess_per_pass: 0x5", 1),
		"a number with a sign":      strings.Replace(tuning, "assess_per_pass: 5", "assess_per_pass: +5", 1),
		"a string as a number":      strings.Replace(tuning, "direction: initial", "direction: 7", 1),
		"a time without its offset": strings.Replace(tuning, "2026-10-15T00:00:00Z", "2026-10-15 00:00:00", 1),
		"a time with a fraction":    strings.Replace(tuning, "2026-10-15T00:00:00Z", "2026-10-15T00:00:00.5Z", 1),
		"version 2 marked initial":  strings.Replace(tuning, "rule_version: 1", "rule_version: 2", 1),
		"version 1 marked loosen":   strings.Replace(tuning, "direction: initial", "direction: loosen", 1),
		"an unknown direction":      strings.Replace(tuning, "direction: initial", "direction: sideways", 1),
		"expiring before it starts": strings.Replace(tuning, "2027-01-15T00:00:00Z", "2026-10-14T00:00:00Z", 1),
		"an unknown kind":           text("visibility", "assess_per_pass: 5\n"),
		"a list where a value goes": strings.Replace(tuning, "assess_per_pass: 5", "assess_per_pass: [5]", 1),
		"a sequence at the top":     "- rule_id: tuning\n",
		"nothing":                   "",
		"text past the limit":       tuning + "# " + strings.Repeat("x", rules.MaxText) + "\n",
	} {
		_, err := rules.Parse([]byte(s))
		if err == nil {
			t.Errorf("%s: parsed\n%s", name, s)
			continue
		}
		t.Logf("%s: %v", name, err)
	}
}

// Tightens compares, it never trusts the declared direction (T35).
func TestTightens(t *testing.T) {
	plan := text("plan-standing", "done_rules: true\nactive_on_lease: false\n")
	next := func(base, from, to string) string {
		s := strings.Replace(base, "rule_version: 1\ndirection: initial", "rule_version: 2\ndirection: tighten", 1)
		if from != "" {
			if !strings.Contains(s, from) {
				t.Fatalf("no %q in\n%s", from, s)
			}
			s = strings.Replace(s, from, to, 1)
		}
		return s
	}
	for name, c := range map[string]struct {
		prev, next string
		tighter    bool
	}{
		"the same rule again": {plan, next(plan, "", ""), true},
		"expiring later":      {plan, next(plan, "2027-01-15T00:00:00Z", "2027-02-15T00:00:00Z"), false},
		"expiring sooner":     {plan, next(plan, "2027-01-15T00:00:00Z", "2026-12-15T00:00:00Z"), true},
		"starting earlier":    {plan, next(plan, "2026-10-15T00:00:00Z", "2026-10-01T00:00:00Z"), false},
		"a different rule":    {tuning, next(plan, "", ""), false},
	} {
		err := rules.Tightens(parse(t, c.prev), parse(t, c.next))
		if (err == nil) != c.tighter {
			t.Errorf("%s: tightens %v (%v); want %v", name, err == nil, err, c.tighter)
		}
	}
}
