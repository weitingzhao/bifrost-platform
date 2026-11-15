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

var (
	envelope = text("standing-envelope:dev", `tenant: dev
max_action: branch
budget:
  max_attempts: 40
  max_wall_hours: 48
executors: [exec:deterministic, exec:codex]
hosts: [mbp-dev]
`)
	budget = text("budget:dev", `tenant: dev
mission: {max_attempts: 40, max_wall_hours: 48}
attempt_hard_limit_s: 3600
`)
)

func parse(t *testing.T, s string) rules.Rule {
	t.Helper()
	r, err := rules.Parse([]byte(s))
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, s)
	}
	return r
}

// Each kind reads, and its body_json is the canonical JSON of what was read.
func TestParseEachKind(t *testing.T) {
	for name, c := range map[string]struct {
		text, kind, body string
	}{
		"envelope": {envelope, rules.KindEnvelope, `{"budget":{"max_attempts":40,"max_wall_hours":48},"direction":"initial","effective_at":"2026-10-15T00:00:00Z","executors":["exec:codex","exec:deterministic"],"expires_at":"2027-01-15T00:00:00Z","hosts":["mbp-dev"],"max_action":"branch","rule_id":"standing-envelope:dev","rule_version":1,"tenant":"dev"}`},
		"budget":   {budget, rules.KindBudget, `{"attempt_hard_limit_s":3600,"direction":"initial","effective_at":"2026-10-15T00:00:00Z","expires_at":"2027-01-15T00:00:00Z","mission":{"max_attempts":40,"max_wall_hours":48},"rule_id":"budget:dev","rule_version":1,"tenant":"dev"}`},
	} {
		r := parse(t, c.text)
		body, err := r.BodyJSON()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if r.Kind != c.kind || string(body) != c.body {
			t.Errorf("%s: kind %s, body\n%s\nwant\n%s", name, r.Kind, body, c.body)
		}
	}
}

// What the closed parser refuses (T35: a field it does not know, and every
// way YAML can make one text mean two things).
func TestParseRefuses(t *testing.T) {
	for name, s := range map[string]string{
		"an unknown field":           envelope + "note: hello\n",
		"an unknown nested field":    strings.Replace(envelope, "  max_wall_hours: 48\n", "  max_wall_hours: 48\n  max_cost: 9\n", 1),
		"an anchor":                  strings.Replace(envelope, "budget:\n", "budget: &b\n", 1),
		"an alias":                   strings.Replace(budget, "mission: {max_attempts: 40, max_wall_hours: 48}", "mission: &m {max_attempts: 40, max_wall_hours: 48}\nother: *m", 1),
		"a merge key":                strings.Replace(budget, "mission: {max_attempts: 40, max_wall_hours: 48}", "mission:\n  <<: {max_attempts: 40}\n  max_wall_hours: 48", 1),
		"an explicit tag":            strings.Replace(tuning, "assess_per_pass: 5", "assess_per_pass: !!int 5", 1),
		"a second document":          tuning + "---\nassess_per_pass: 5\n",
		"a key given twice":          tuning + "  assess_per_pass: 4\n",
		"a number as a string":       strings.Replace(tuning, "assess_per_pass: 5", `assess_per_pass: "5"`, 1),
		"a number not in decimal":    strings.Replace(budget, "attempt_hard_limit_s: 3600", "attempt_hard_limit_s: 0xE10", 1),
		"a number with a sign":       strings.Replace(budget, "attempt_hard_limit_s: 3600", "attempt_hard_limit_s: +3600", 1),
		"a string as a number":       strings.Replace(envelope, "max_action: branch", "max_action: 7", 1),
		"a time without its offset":  strings.Replace(tuning, "2026-10-15T00:00:00Z", "2026-10-15 00:00:00", 1),
		"a time with a fraction":     strings.Replace(tuning, "2026-10-15T00:00:00Z", "2026-10-15T00:00:00.5Z", 1),
		"version 2 marked initial":   strings.Replace(tuning, "rule_version: 1", "rule_version: 2", 1),
		"version 1 marked loosen":    strings.Replace(tuning, "direction: initial", "direction: loosen", 1),
		"an unknown direction":       strings.Replace(tuning, "direction: initial", "direction: sideways", 1),
		"expiring before it starts":  strings.Replace(tuning, "2027-01-15T00:00:00Z", "2026-10-14T00:00:00Z", 1),
		"another tenant in the body": strings.Replace(envelope, "tenant: dev", "tenant: business", 1),
		"an unknown kind":            text("visibility", "assess_per_pass: 5\n"),
		"a tenant nobody has":        text("budget:prod", "tenant: prod\n"),
		"a C-class action":           strings.Replace(envelope, "max_action: branch", "max_action: release", 1),
		"a budget past its bound":    strings.Replace(envelope, "max_attempts: 40", "max_attempts: 100000", 1),
		"no executors":               strings.Replace(envelope, "executors: [exec:deterministic, exec:codex]\n", "", 1),
		"an executor twice":          strings.Replace(envelope, "[exec:deterministic, exec:codex]", "[exec:codex, exec:codex]", 1),
		"a list where a value goes":  strings.Replace(tuning, "assess_per_pass: 5", "assess_per_pass: [5]", 1),
		"a sequence at the top":      "- rule_id: tuning\n",
		"nothing":                    "",
		"text past the limit":        tuning + "# " + strings.Repeat("x", rules.MaxText) + "\n",
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
		"the same rule again":     {envelope, next(envelope, "", ""), true},
		"a smaller budget":        {envelope, next(envelope, "max_attempts: 40", "max_attempts: 20"), true},
		"a larger budget":         {envelope, next(envelope, "max_attempts: 40", "max_attempts: 41"), false},
		"one executor fewer":      {envelope, next(envelope, "[exec:deterministic, exec:codex]", "[exec:codex]"), true},
		"another executor":        {envelope, next(envelope, "[exec:deterministic, exec:codex]", "[exec:codex, exec:claude]"), false},
		"another host":            {envelope, next(envelope, "[mbp-dev]", "[mbp-dev, mini]"), false},
		"no action at all":        {envelope, next(envelope, "max_action: branch", "max_action: none"), true},
		"expiring later":          {envelope, next(envelope, "2027-01-15T00:00:00Z", "2027-02-15T00:00:00Z"), false},
		"expiring sooner":         {envelope, next(envelope, "2027-01-15T00:00:00Z", "2026-12-15T00:00:00Z"), true},
		"starting earlier":        {envelope, next(envelope, "2026-10-15T00:00:00Z", "2026-10-01T00:00:00Z"), false},
		"a shorter attempt limit": {budget, next(budget, "attempt_hard_limit_s: 3600", "attempt_hard_limit_s: 1800"), true},
		"a longer attempt limit":  {budget, next(budget, "attempt_hard_limit_s: 3600", "attempt_hard_limit_s: 7200"), false},
		"a different rule":        {tuning, strings.Replace(next(budget, "", ""), "", "", 1), false},
	} {
		err := rules.Tightens(parse(t, c.prev), parse(t, c.next))
		if (err == nil) != c.tighter {
			t.Errorf("%s: tightens %v (%v); want %v", name, err == nil, err, c.tighter)
		}
	}
}

// A budget rule's auto_grant is optional: stated, it is read (0 allowed) and
// written into the doc; absent, the cap is AutoGrantDefault and the doc stays
// as it was.
func TestBudgetAutoGrant(t *testing.T) {
	r := parse(t, budget)
	if r.Budget.AutoGrantStated || r.Budget.AutoGrant != rules.AutoGrantDefault || rules.AutoGrantDefault != (rules.Budget{MaxAttempts: 30, MaxWallHours: 24}) {
		t.Errorf("no auto_grant: %+v, want the default 30/24 unstated", r.Budget)
	}
	r = parse(t, budget+"auto_grant: {max_attempts: 6, max_wall_hours: 2}\n")
	if !r.Budget.AutoGrantStated || r.Budget.AutoGrant != (rules.Budget{MaxAttempts: 6, MaxWallHours: 2}) {
		t.Errorf("auto_grant 6/2: %+v", r.Budget)
	}
	if body, err := r.BodyJSON(); err != nil || !strings.Contains(string(body), `"auto_grant":{"max_attempts":6,"max_wall_hours":2}`) {
		t.Errorf("body %s has no auto_grant", body)
	}
	r = parse(t, budget+"auto_grant: {max_attempts: 0, max_wall_hours: 0}\n")
	if !r.Budget.AutoGrantStated || r.Budget.AutoGrant != (rules.Budget{}) {
		t.Errorf("auto_grant 0/0: %+v", r.Budget)
	}
	for name, s := range map[string]string{
		"a negative cap":   budget + "auto_grant: {max_attempts: -1, max_wall_hours: 0}\n",
		"a cap past bound": budget + "auto_grant: {max_attempts: 10001, max_wall_hours: 0}\n",
		"hours left out":   budget + "auto_grant: {max_attempts: 3}\n",
		"an unknown field": budget + "auto_grant: {max_attempts: 3, max_wall_hours: 1, rounds: 2}\n",
		"not a mapping":    budget + "auto_grant: 3\n",
	} {
		if _, err := rules.Parse([]byte(s)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

// Raising auto_grant loosens, lowering it tightens; an unstated one is the
// default's.
func TestBudgetAutoGrantTightens(t *testing.T) {
	next := func(s string) string {
		return strings.Replace(s, "rule_version: 1\ndirection: initial", "rule_version: 2\ndirection: tighten", 1)
	}
	with := func(a, h string) string {
		return budget + "auto_grant: {max_attempts: " + a + ", max_wall_hours: " + h + "}\n"
	}
	for name, c := range map[string]struct {
		prev, next string
		tighter    bool
	}{
		"lowered attempts":      {with("6", "2"), next(with("3", "2")), true},
		"lowered to 0":          {with("6", "2"), next(with("0", "0")), true},
		"raised attempts":       {with("6", "2"), next(with("9", "2")), false},
		"raised hours":          {with("6", "2"), next(with("6", "3")), false},
		"stated under default":  {budget, next(with("30", "24")), true},
		"stated over default":   {budget, next(with("31", "24")), false},
		"dropped, so default":   {with("6", "2"), next(budget), false},
		"dropped from default+": {with("40", "48"), next(budget), true},
	} {
		err := rules.Tightens(parse(t, c.prev), parse(t, c.next))
		if (err == nil) != c.tighter {
			t.Errorf("%s: tightens %v (%v); want %v", name, err == nil, err, c.tighter)
		}
	}
}
