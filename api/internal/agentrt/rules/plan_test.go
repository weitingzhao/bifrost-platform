package rules_test

import (
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

func planText(version int, direction, doneRules, activeOnLease string) string {
	return "rule_id: plan-standing\nrule_version: " + map[int]string{1: "1", 2: "2"}[version] + "\ndirection: " + direction +
		"\neffective_at: 2026-10-20T00:00:00Z\nexpires_at: 2027-01-20T00:00:00Z\ndone_rules: " + doneRules + "\nactive_on_lease: " + activeOnLease + "\n"
}

// The plan's standing authorization (CUT-plan-import-2026-10-19.md §7): two
// grants, each true or false; read closed, written back as the text says.
func TestPlanStandingRule(t *testing.T) {
	r, err := rules.Parse([]byte(planText(1, "initial", "true", "false")))
	if err != nil {
		t.Fatal(err)
	}
	if r.Kind != rules.KindPlan || r.Plan == nil || !r.Plan.DoneRules || r.Plan.ActiveOnLease {
		t.Fatalf("parsed = %+v %+v", r, r.Plan)
	}
	body, err := r.BodyJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"active_on_lease":false`) || !strings.Contains(string(body), `"done_rules":true`) {
		t.Fatalf("body = %s", body)
	}
	for name, text := range map[string]string{
		"a string":      planText(1, "initial", `"yes"`, "false"),
		"a number":      planText(1, "initial", "1", "false"),
		"missing":       strings.Replace(planText(1, "initial", "true", "false"), "active_on_lease: false\n", "", 1),
		"another field": planText(1, "initial", "true", "false") + "set_marks: true\n",
		"an argument":   strings.Replace(planText(1, "initial", "true", "false"), "rule_id: plan-standing", "rule_id: plan-standing:R2", 1),
	} {
		if _, err := rules.Parse([]byte(text)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

// Taking a grant away tightens; giving one loosens and needs the Owner.
func TestPlanStandingTightens(t *testing.T) {
	parse := func(text string) rules.Rule {
		t.Helper()
		r, err := rules.Parse([]byte(text))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	both := parse(planText(1, "initial", "true", "true"))
	if err := rules.Tightens(both, parse(planText(2, "tighten", "false", "true"))); err != nil {
		t.Fatalf("dropping done_rules: %v", err)
	}
	if err := rules.Tightens(both, parse(planText(2, "tighten", "true", "true"))); err != nil {
		t.Fatalf("the same grants: %v", err)
	}
	none := parse(planText(1, "initial", "false", "false"))
	for _, next := range []string{planText(2, "loosen", "true", "false"), planText(2, "loosen", "false", "true")} {
		if err := rules.Tightens(none, parse(next)); err == nil {
			t.Errorf("a grant added counted as tightening: %s", next)
		}
	}
}
