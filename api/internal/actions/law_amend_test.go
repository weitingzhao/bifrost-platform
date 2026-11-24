package actions

import (
	"context"
	"errors"
	"testing"
)

// 2026-11-23, Owner: only a change of law opens a ticket. The gate is the
// approvals line's: law_amend is tier D and runs nothing. What a card says is
// legislation's (planstore lawcard_test.go); without its rules wired the gate
// refuses every card (moved 2026-11-24, legislation and administration
// agreed: no law ticket stands alone).
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

func TestLawAmendGateUsesLegislationsRules(t *testing.T) {
	add := map[string]any{"op": "add", "after": "x", "revert": "r"}
	SetLawCard(nil)
	if err := CheckLawAmend(context.Background(), add); err == nil {
		t.Fatal("card accepted with no rules wired")
	}
	refused := errors.New("refused by legislation")
	SetLawCard(&LawCard{
		Check:   func(context.Context, map[string]any) error { return refused },
		Summary: func(context.Context, map[string]any) string { return "Law add: will become law 80" },
		Read:    func(map[string]any) (any, error) { return "read", nil },
	})
	t.Cleanup(func() { SetLawCard(nil) })
	if err := CheckLawAmend(context.Background(), add); !errors.Is(err, refused) {
		t.Fatalf("check = %v; want legislation's answer", err)
	}
	if got := lawAmendSummary(context.Background(), add); got != "Law add: will become law 80" {
		t.Fatalf("summary = %q", got)
	}
	out, _ := ExecuteLawAmend(context.Background(), add)
	if out.(map[string]any)["law_amend"] != "read" {
		t.Fatalf("executor did not record legislation's reading: %v", out)
	}
}
