package approvals

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ISSUE-105 (2026-11-25): a line finds the Owner's decision for its own
// change by the item, and only an executed decision counts. A decision card
// goes with the law tickets, not the daily cards.
func TestADecisionIsFoundByItemOnlyOnceExecuted(t *testing.T) {
	if TrackOf(OwnerDecisionAction) != TrackLaw {
		t.Fatalf("owner_decision track = %s", TrackOf(OwnerDecisionAction))
	}
	executed := Approval{Action: OwnerDecisionAction, Status: StatusExecuted, Params: map[string]any{"items": []any{"wall:106", "wall:110"}}}
	if !namesItem(executed, "wall:110") || namesItem(executed, "wall:112") {
		t.Fatal("an executed decision names exactly its items")
	}
	// Positive control for the case above: the same scope, not yet approved.
	pending := executed
	pending.Status = StatusPending
	if namesItem(pending, "wall:110") {
		t.Fatal("a pending decision is not a decision")
	}
	other := Approval{Action: "law_amend", Status: StatusExecuted, Params: map[string]any{"items": []any{"wall:110"}}}
	if namesItem(other, "wall:110") {
		t.Fatal("only owner_decision cards are decisions")
	}

	s, _ := lifetimeService(t)
	res := s.create(context.Background(), "law", OwnerDecisionAction, "decision", "", map[string]any{
		"items": []any{"wall:106"}, "law": "LAW-6", "source": "thread", "text": "Lower it", "revert": "Repeal it",
	})
	if res.Status != http.StatusCreated {
		t.Fatalf("create = %d %v", res.Status, res.Body)
	}
	w := httptest.NewRecorder()
	s.HandleList(w, httptest.NewRequest(http.MethodGet, "/api/v1/approvals?item=wall:106", nil))
	var out struct{ Approvals []map[string]any }
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != http.StatusOK || len(out.Approvals) != 0 {
		t.Fatalf("pending decision listed by item: %d %v", w.Code, out.Approvals)
	}
}
