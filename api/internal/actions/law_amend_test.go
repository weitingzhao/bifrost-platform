package actions

import (
	"context"
	"testing"
)

// 2026-11-23, Owner: only a change of law opens a ticket, and the ticket names
// add/amend/repeal and measurable standards (1 Plan and the approvals line
// agreed the params that day). A card that does not read must be refused at
// creation, not after the Owner approved it.
// fakeRegister holds law 79 in force ("a"), law 5 repealed, next number 80.
func fakeRegister(t *testing.T, next string) {
	t.Helper()
	SetLawRegister(&LawRegister{
		Current: func(_ context.Context, n string) (string, string, bool, error) {
			switch n {
			case "79":
				return "a\n", "已落", true, nil
			case "5":
				return "old", lawRepealed, true, nil
			}
			return "", "", false, nil
		},
		Next: func(context.Context) (string, error) { return next, nil },
	})
	t.Cleanup(func() { SetLawRegister(nil) })
}

func TestLawAmendReads(t *testing.T) {
	fakeRegister(t, "80")
	std := []any{map[string]any{"id": "s1", "standard": "C cards per week fall", "measure": "count approval.card.created"}}
	good := []map[string]any{
		{"op": "add", "after": "text", "standards": std, "revert": "repeal it"},
		{"op": "amend", "law": "79", "before": "a", "after": "b", "standards": std, "revert": "amend back"},
		{"op": "repeal", "law": "79", "before": "a", "revert": "add it again"},
		{"op": "add", "law": "80", "after": "text", "standards": std, "revert": "repeal it"},
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

// 2026-11-23, Owner (relayed by 1 Plan): a ticket changes a real record of
// the law in the register, never stands alone.
func TestLawAmendMustMatchTheRegister(t *testing.T) {
	std := []any{map[string]any{"id": "s1", "standard": "x", "measure": "y"}}
	fakeRegister(t, "80")
	refused := map[string]map[string]any{
		"law not in register":   {"op": "amend", "law": "999", "before": "a", "after": "b", "standards": std, "revert": "r"},
		"before differs":        {"op": "amend", "law": "79", "before": "stale", "after": "b", "standards": std, "revert": "r"},
		"already repealed":      {"op": "repeal", "law": "5", "before": "old", "revert": "r"},
		"add with wrong number": {"op": "add", "law": "81", "after": "x", "standards": std, "revert": "r"},
	}
	for name, p := range refused {
		if err := CheckLawAmend(context.Background(), p); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
	add := map[string]any{"op": "add", "after": "x", "standards": std, "revert": "r"}
	if got := lawAmendSummary(context.Background(), add); got != "Law add: will become law 80" {
		t.Fatalf("add summary = %q", got)
	}
	fakeRegister(t, "")
	if err := CheckLawAmend(context.Background(), add); err == nil {
		t.Fatal("add accepted while the register is empty")
	}
	SetLawRegister(nil)
	if err := CheckLawAmend(context.Background(), add); err == nil {
		t.Fatal("card accepted with no register wired")
	}
}
