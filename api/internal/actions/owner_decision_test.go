package actions

import (
	"context"
	"strings"
	"testing"
)

// ISSUE-105 (2026-11-25): after the Owner approved 31 loosenings in one
// thread, each line came back asking for a record it could find for its own
// change. The card's scope is a list of named items; a card without one, or
// with a blank or repeated item, is refused at creation.
func TestOwnerDecisionNeedsANamedScope(t *testing.T) {
	ok := map[string]any{"items": []any{"wall:106", "wall:110"}, "law": "LAW-6", "source": "owner_said 1a2b", "text": "Lower these to B", "revert": "Repeal it"}
	if err := CheckOwnerDecision(context.Background(), ok); err != nil {
		t.Fatalf("a complete card: %v", err)
	}
	for name, mut := range map[string]func(map[string]any){
		"no items":     func(p map[string]any) { delete(p, "items") },
		"blank item":   func(p map[string]any) { p["items"] = []any{"wall:106", ""} },
		"padded item":  func(p map[string]any) { p["items"] = []any{" wall:106"} },
		"twice":        func(p map[string]any) { p["items"] = []any{"wall:106", "wall:106"} },
		"no law":       func(p map[string]any) { delete(p, "law") },
		"no source":    func(p map[string]any) { p["source"] = " " },
		"no text":      func(p map[string]any) { delete(p, "text") },
		"long text":    func(p map[string]any) { p["text"] = strings.Repeat("x", decisionMaxText+1) },
		"not a string": func(p map[string]any) { p["items"] = []any{106} },
	} {
		p := map[string]any{}
		for k, v := range ok {
			p[k] = v
		}
		mut(p)
		if err := CheckOwnerDecision(context.Background(), p); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if a, found := ByID(OwnerDecisionAction); !found || a.Tier != TierD || a.Runner == RunnerOwner {
		t.Fatalf("catalog entry = %+v %v", a, found)
	}
}
