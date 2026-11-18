package unitexec

import (
	"strings"
	"testing"
)

// Origin: 2026-11-18, the unit TaskRuns move to a cache agreed with the CI
// line: the cache must answer only the selectors it can answer exactly, a
// narrower one would be served every unit's runs.
func TestOnlyThePlanSelectorsGoToTheCache(t *testing.T) {
	for _, c := range []struct {
		label, selector, plan string
		ok                    bool
	}{
		{LabelPlan, "", "", true},
		{LabelPlan, LabelPlan, "", true},
		{LabelPlan, LabelPlan + "=p1", "p1", true},
		{LabelPlan, LabelPlan + "=p1,other=x", "", false},
		{LabelPlan, LabelPlan + "=", "", false},
		{LabelPlan, "other=x", "", false},
		{"", "tekton.dev/pipelineRun=r", "", false},
	} {
		if plan, ok := PlanOfSelector(c.label, c.selector); plan != c.plan || ok != c.ok {
			t.Errorf("%q %q: got %q %v", c.label, c.selector, plan, ok)
		}
	}
}

// Origin: 2026-11-18, same change: the comparison is over what the evidence
// reads, so a difference there must show and one elsewhere must not.
func TestTheCompareKeyIsWhatTheEvidenceReads(t *testing.T) {
	run := func(message, ignored string) map[string]any {
		return map[string]any{"metadata": map[string]any{"name": "u", "uid": "1"}, "ignored": ignored,
			"status": map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "message": message}}}}
	}
	if CompareKey("u", run("m", "a")) != CompareKey("u", run("m", "b")) {
		t.Error("a field the evidence does not read changed the key")
	}
	if CompareKey("u", run("m", "a")) == CompareKey("u", run("other", "a")) {
		t.Error("a condition message the evidence reads did not change the key")
	}
	if !strings.HasPrefix(CompareKey("u", run("m", "a")), "u\x00") {
		t.Error("the key must start with the name")
	}
}
