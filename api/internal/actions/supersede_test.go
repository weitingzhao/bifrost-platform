package actions

import (
	"context"
	"testing"
)

func TestSupersedeCatalogPinsEntries(t *testing.T) {
	a, ok := ByID("supersede_agent_tasks")
	if !ok || a.Tier != TierD {
		t.Fatal("superseding is not Owner-approved")
	}
	entry := func(commit string) map[string]any {
		return map[string]any{"env": "prod", "reason": "replaced", "entries": []any{map[string]any{"task_id": "old", "version": 1, "replacement_task_id": "new", "commit": commit}}}
	}
	p, err := a.Normalize(entry("a"))
	if err != nil {
		t.Fatal(err)
	}
	q, err := a.Normalize(entry("b"))
	if err != nil {
		t.Fatal(err)
	}
	hp, _ := ParamsHash(p)
	hq, _ := ParamsHash(q)
	if hp == hq {
		t.Fatal("replacement commit not bound to approval")
	}
	if a.Missing(p) != "" || a.Describe(context.Background(), p, "").Env != "prod" {
		t.Fatal("missing approval material")
	}
}
