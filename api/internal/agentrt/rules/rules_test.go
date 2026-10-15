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
	concurrency = text("concurrency", "per_host: 1\n")
	budget      = text("budget:dev", `tenant: dev
mission: {max_attempts: 40, max_wall_hours: 48}
attempt_hard_limit_s: 3600
`)
	job = text("job:implementer", `tenant: dev
text: |
  Implement the task's spec on a branch.
candidates:
  - executor: exec:codex
    provider: openai
    model_pattern: "*"
    status: active
  - executor: exec:cursor
    provider: cursor-mix
    model_pattern: "*"
    status: probation
`)
	facts = text("capability-facts", `facts:
  - executor: exec:codex
    kind: cannot
    subject: docker
  - executor: exec:cursor
    provider: cursor-mix
    kind: prefer
    subject: frontend
    scope: {repo: bifrost-trade-frontend}
`)
	routing = text("routing:dev", `tenant: dev
order: [capability, quota]
quota_stop_percent: 90
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
		"envelope":    {envelope, rules.KindEnvelope, `{"budget":{"max_attempts":40,"max_wall_hours":48},"direction":"initial","effective_at":"2026-10-15T00:00:00Z","executors":["exec:codex","exec:deterministic"],"expires_at":"2027-01-15T00:00:00Z","hosts":["mbp-dev"],"max_action":"branch","rule_id":"standing-envelope:dev","rule_version":1,"tenant":"dev"}`},
		"concurrency": {concurrency, rules.KindConcurrency, `{"direction":"initial","effective_at":"2026-10-15T00:00:00Z","expires_at":"2027-01-15T00:00:00Z","per_host":1,"rule_id":"concurrency","rule_version":1}`},
		"budget":      {budget, rules.KindBudget, `{"attempt_hard_limit_s":3600,"direction":"initial","effective_at":"2026-10-15T00:00:00Z","expires_at":"2027-01-15T00:00:00Z","mission":{"max_attempts":40,"max_wall_hours":48},"rule_id":"budget:dev","rule_version":1,"tenant":"dev"}`},
		"job":         {job, rules.KindJob, `{"candidates":[{"executor":"exec:codex","model_pattern":"*","provider":"openai","status":"active"},{"executor":"exec:cursor","model_pattern":"*","provider":"cursor-mix","status":"probation"}],"direction":"initial","effective_at":"2026-10-15T00:00:00Z","expires_at":"2027-01-15T00:00:00Z","rule_id":"job:implementer","rule_version":1,"tenant":"dev","text":"Implement the task's spec on a branch.\n"}`},
		"facts":       {facts, rules.KindFacts, `{"direction":"initial","effective_at":"2026-10-15T00:00:00Z","expires_at":"2027-01-15T00:00:00Z","facts":[{"executor":"exec:codex","kind":"cannot","subject":"docker"},{"executor":"exec:cursor","kind":"prefer","provider":"cursor-mix","scope":{"repo":"bifrost-trade-frontend"},"subject":"frontend"}],"rule_id":"capability-facts","rule_version":1}`},
		"routing":     {routing, rules.KindRouting, `{"direction":"initial","effective_at":"2026-10-15T00:00:00Z","expires_at":"2027-01-15T00:00:00Z","order":["capability","quota"],"quota_stop_percent":90,"rule_id":"routing:dev","rule_version":1,"tenant":"dev"}`},
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
		"an explicit tag":            strings.Replace(concurrency, "per_host: 1", "per_host: !!int 1", 1),
		"a second document":          concurrency + "---\nper_host: 1\n",
		"a key given twice":          concurrency + "per_host: 0\n",
		"a number as a string":       strings.Replace(concurrency, "per_host: 1", `per_host: "1"`, 1),
		"a number not in decimal":    strings.Replace(budget, "attempt_hard_limit_s: 3600", "attempt_hard_limit_s: 0xE10", 1),
		"a string as a number":       strings.Replace(envelope, "max_action: branch", "max_action: 7", 1),
		"a time without its offset":  strings.Replace(concurrency, "2026-10-15T00:00:00Z", "2026-10-15 00:00:00", 1),
		"version 2 marked initial":   strings.Replace(concurrency, "rule_version: 1", "rule_version: 2", 1),
		"version 1 marked loosen":    strings.Replace(concurrency, "direction: initial", "direction: loosen", 1),
		"an unknown direction":       strings.Replace(concurrency, "direction: initial", "direction: sideways", 1),
		"expiring before it starts":  strings.Replace(concurrency, "2027-01-15T00:00:00Z", "2026-10-14T00:00:00Z", 1),
		"another tenant in the body": strings.Replace(envelope, "tenant: dev", "tenant: business", 1),
		"an unknown kind":            text("visibility", "per_host: 1\n"),
		"a tenant nobody has":        text("budget:prod", "tenant: prod\n"),
		"a C-class action":           strings.Replace(envelope, "max_action: branch", "max_action: release", 1),
		"two live attempts a host":   strings.Replace(concurrency, "per_host: 1", "per_host: 2", 1),
		"quota before capability":    strings.Replace(routing, "[capability, quota]", "[quota, capability]", 1),
		"a budget past its bound":    strings.Replace(envelope, "max_attempts: 40", "max_attempts: 100000", 1),
		"no executors":               strings.Replace(envelope, "executors: [exec:deterministic, exec:codex]\n", "", 1),
		"an executor twice":          strings.Replace(envelope, "[exec:deterministic, exec:codex]", "[exec:codex, exec:codex]", 1),
		"a candidate status unknown": strings.Replace(job, "status: probation", "status: trusted", 1),
		"a candidate twice":          strings.Replace(job, "exec:cursor\n    provider: cursor-mix", "exec:codex\n    provider: openai", 1),
		"a fact kind unknown":        strings.Replace(facts, "kind: prefer", "kind: maybe", 1),
		"an empty brief":             strings.Replace(job, "text: |\n  Implement the task's spec on a branch.\n", "text: \"  \"\n", 1),
		"a list where a value goes":  strings.Replace(concurrency, "per_host: 1", "per_host: [1]", 1),
		"a sequence at the top":      "- rule_id: concurrency\n",
		"nothing":                    "",
		"text past the limit":        concurrency + "# " + strings.Repeat("x", rules.MaxText) + "\n",
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
		"the same rule again":       {envelope, next(envelope, "", ""), true},
		"a smaller budget":          {envelope, next(envelope, "max_attempts: 40", "max_attempts: 20"), true},
		"a larger budget":           {envelope, next(envelope, "max_attempts: 40", "max_attempts: 41"), false},
		"one executor fewer":        {envelope, next(envelope, "[exec:deterministic, exec:codex]", "[exec:codex]"), true},
		"another executor":          {envelope, next(envelope, "[exec:deterministic, exec:codex]", "[exec:codex, exec:claude]"), false},
		"another host":              {envelope, next(envelope, "[mbp-dev]", "[mbp-dev, mini]"), false},
		"no action at all":          {envelope, next(envelope, "max_action: branch", "max_action: none"), true},
		"expiring later":            {envelope, next(envelope, "2027-01-15T00:00:00Z", "2027-02-15T00:00:00Z"), false},
		"expiring sooner":           {envelope, next(envelope, "2027-01-15T00:00:00Z", "2026-12-15T00:00:00Z"), true},
		"starting earlier":          {envelope, next(envelope, "2026-10-15T00:00:00Z", "2026-10-01T00:00:00Z"), false},
		"no concurrency":            {concurrency, next(concurrency, "per_host: 1", "per_host: 0"), true},
		"concurrency back up":       {next(concurrency, "per_host: 1", "per_host: 0"), strings.Replace(next(concurrency, "", ""), "rule_version: 2", "rule_version: 3", 1), false},
		"a shorter attempt limit":   {budget, next(budget, "attempt_hard_limit_s: 3600", "attempt_hard_limit_s: 1800"), true},
		"a longer attempt limit":    {budget, next(budget, "attempt_hard_limit_s: 3600", "attempt_hard_limit_s: 7200"), false},
		"a candidate demoted":       {job, next(job, "status: active", "status: demoted"), true},
		"a candidate left out":      {job, next(job, "  - executor: exec:cursor\n    provider: cursor-mix\n    model_pattern: \"*\"\n    status: probation\n", ""), true},
		"a candidate promoted":      {job, next(job, "status: probation", "status: active"), false},
		"a new candidate":           {job, next(job, "model_pattern: \"*\"\n    status: probation", "model_pattern: \"gpt-*\"\n    status: probation"), false},
		"the brief rewritten":       {job, next(job, "Implement the task's spec", "Implement anything"), false},
		"a fact added that forbids": {facts, next(facts, "facts:\n", "facts:\n  - executor: exec:claude\n    kind: must_not\n    subject: release\n"), true},
		"a fact added that prefers": {facts, next(facts, "facts:\n", "facts:\n  - executor: exec:claude\n    kind: prefer\n    subject: go\n"), false},
		"a fact dropped":            {facts, next(facts, "  - executor: exec:codex\n    kind: cannot\n    subject: docker\n", ""), false},
		"a fact's scope changed":    {facts, next(facts, "repo: bifrost-trade-frontend", "repo: bifrost-platform"), false},
		"a lower quota stop":        {routing, next(routing, "quota_stop_percent: 90", "quota_stop_percent: 80"), true},
		"a higher quota stop":       {routing, next(routing, "quota_stop_percent: 90", "quota_stop_percent: 95"), false},
		"a different rule":          {concurrency, strings.Replace(next(budget, "", ""), "", "", 1), false},
	} {
		err := rules.Tightens(parse(t, c.prev), parse(t, c.next))
		if (err == nil) != c.tighter {
			t.Errorf("%s: tightens %v (%v); want %v", name, err == nil, err, c.tighter)
		}
	}
}
