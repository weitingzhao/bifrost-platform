package actions

import (
	"context"
	"testing"
)

// 2026-11-23, Owner: only a change of law opens a ticket, and the ticket names
// add/amend/repeal and measurable standards (1 Plan and the approvals line
// agreed the params that day). A card that does not read must be refused at
// creation, not after the Owner approved it.
func TestLawAmendReads(t *testing.T) {
	std := []any{map[string]any{"id": "s1", "standard": "C cards per week fall", "measure": "count approval.card.created"}}
	good := []map[string]any{
		{"op": "add", "after": "text", "standards": std, "revert": "repeal it"},
		{"op": "amend", "law": "79", "before": "a", "after": "b", "standards": std, "revert": "amend back"},
		{"op": "repeal", "law": "79", "before": "a", "revert": "add it again"},
	}
	for i, p := range good {
		a, _ := ByID(LawAmendAction)
		n, err := a.Normalize(p)
		if err != nil {
			t.Fatalf("good %d normalize: %v", i, err)
		}
		if err := CheckLawAmend(context.Background(), n); err != nil {
			t.Fatalf("good %d refused: %v", i, err)
		}
	}
	dup := []any{std[0], std[0]}
	bad := []map[string]any{
		{"op": "change", "after": "x", "standards": std, "revert": "r"},
		{"op": "add", "after": "x", "revert": "r"},                                  // no standard
		{"op": "add", "after": "x", "standards": std},                               // no revert
		{"op": "amend", "law": "79", "after": "b", "standards": std, "revert": "r"}, // no before
		{"op": "repeal", "law": "79", "before": "a", "after": "b", "revert": "r"},   // after on repeal
		{"op": "add", "after": "x", "standards": dup, "revert": "r"},                // id twice
		{"op": "add", "after": "x", "standards": []any{map[string]any{"id": "s1"}}, "revert": "r"},
	}
	for i, p := range bad {
		if err := CheckLawAmend(context.Background(), p); err == nil {
			t.Fatalf("bad %d was accepted: %v", i, p)
		}
	}
}

func TestLawAmendIsTierDAndRunsNothing(t *testing.T) {
	a, _ := ByID(LawAmendAction)
	if a.ID == "" || a.Tier != TierD || a.Method != "" {
		t.Fatalf("law_amend must be a tier D catalog action with no route: %+v", a)
	}
	out, err := ExecuteLawAmend(context.Background(), map[string]any{"op": "repeal", "law": "1", "before": "a", "revert": "r"})
	if err != nil {
		t.Fatal(err)
	}
	if m := out.(map[string]any); m["executed_by_platform"] != false {
		t.Fatalf("law_amend must not claim the platform did anything: %v", m)
	}
}
