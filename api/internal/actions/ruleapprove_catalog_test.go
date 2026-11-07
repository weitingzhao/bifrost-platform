package actions

import (
	"context"
	"strings"
	"testing"
)

// agentrt_rule_approve: tier D, its four closed parameters, its card line
// the rule, the version and each change, old and new (Q2 A).
func TestRuleApproveCatalogAndCard(t *testing.T) {
	a, ok := ByID("agentrt_rule_approve")
	if !ok || a.Tier != TierD || a.Runner != "" || len(a.Params) != 4 {
		t.Fatalf("catalog entry %+v", a)
	}
	for _, p := range a.Params {
		if !p.Required || p.In != "body" {
			t.Fatalf("param %+v", p)
		}
	}
	params, err := a.Normalize(map[string]any{"env": "prod", "rule_id": "concurrency", "rule_version": 2.0, "text_artifact_id": "sha256:ab"})
	if err != nil || params["rule_version"] != 2 {
		t.Fatalf("normalized %v (%v)", params, err)
	}
	SetRuleChangeLookup(func(context.Context, map[string]any) ([]RuleChange, error) {
		return []RuleChange{{Field: "per_host", Old: "1", New: "3"}}, nil
	})
	t.Cleanup(func() { SetRuleChangeLookup(nil) })
	d := a.Describe(context.Background(), params, "more slots")
	if d.Env != "prod" || d.Summary != "Capacity change: concurrency v2: per_host 1 → 3" {
		t.Fatalf("card %+v", d)
	}
	if d.KeyParams["rule_id"] != "concurrency" || d.KeyParams["rule_version"] != "2" || strings.Contains(d.Summary, "sha256") {
		t.Fatalf("key params %v", d.KeyParams)
	}
	SetRuleChangeLookup(nil)
	if d = a.Describe(context.Background(), params, "more slots"); d.Summary != "Capacity change: concurrency v2" {
		t.Fatalf("card without a reader %+v", d)
	}
}
