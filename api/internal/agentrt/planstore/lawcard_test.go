package planstore_test

import (
	"context"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
)

// Moved from actions/law_amend_test.go on 2026-11-24 with the rules
// (legislation and administration agreed the split). 2026-11-23, Owner: only
// a change of law opens a ticket; it names add/amend/repeal and measurable
// standards; it changes a real record of the law in the register, never
// stands alone. fakeLookup holds law 79 in force ("a"), law 5 repealed.
func fakeLookup(next string) planstore.LawLookup {
	return planstore.LawLookup{
		Current: func(_ context.Context, n string) (planstore.Law, bool, error) {
			switch n {
			case "79":
				return planstore.Law{Law: "79", Text: "a\n", Status: "已落"}, true, nil
			case "5":
				return planstore.Law{Law: "5", Text: "old", Status: planstore.RepealedStatus}, true, nil
			}
			return planstore.Law{}, false, nil
		},
		Next: func(context.Context) (string, error) { return next, nil },
	}
}

func TestLawCardReads(t *testing.T) {
	reg := fakeLookup("80")
	std := []any{map[string]any{"id": "s1", "standard": "C cards per week fall", "measure": "count approval.card.created"}}
	good := []map[string]any{
		{"op": "add", "after": "text", "standards": std, "revert": "repeal it"},
		{"op": "amend", "law": "79", "before": "a", "after": "b", "standards": std, "revert": "amend back"},
		{"op": "repeal", "law": "79", "before": "a", "revert": "add it again"},
		{"op": "add", "law": "80", "after": "text", "standards": std, "revert": "repeal it"},
	}
	for i, p := range good {
		if err := planstore.CheckLawCard(context.Background(), reg, p); err != nil {
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
		if err := planstore.CheckLawCard(context.Background(), reg, p); err == nil {
			t.Fatalf("bad %d was accepted: %v", i, p)
		}
	}
}

func TestLawCardMustMatchTheRegister(t *testing.T) {
	std := []any{map[string]any{"id": "s1", "standard": "x", "measure": "y"}}
	reg := fakeLookup("80")
	refused := map[string]map[string]any{
		"law not in register":   {"op": "amend", "law": "999", "before": "a", "after": "b", "standards": std, "revert": "r"},
		"before differs":        {"op": "amend", "law": "79", "before": "stale", "after": "b", "standards": std, "revert": "r"},
		"already repealed":      {"op": "repeal", "law": "5", "before": "old", "revert": "r"},
		"add with wrong number": {"op": "add", "law": "81", "after": "x", "standards": std, "revert": "r"},
	}
	for name, p := range refused {
		if err := planstore.CheckLawCard(context.Background(), reg, p); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
	add := map[string]any{"op": "add", "after": "x", "standards": std, "revert": "r"}
	if got := planstore.LawCardSummary(context.Background(), reg, add); got != "Law add: will become law 80" {
		t.Fatalf("add summary = %q", got)
	}
	if err := planstore.CheckLawCard(context.Background(), fakeLookup(""), add); err == nil {
		t.Fatal("add accepted while the register is empty")
	}
	if err := planstore.CheckLawCard(context.Background(), planstore.LawLookup{}, add); err == nil {
		t.Fatal("card accepted with no register wired")
	}
}
