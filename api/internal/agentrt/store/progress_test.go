//bifrost:owner plan

package store_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

type progressDoc struct {
	AsOf     int64 `json:"as_of_event_id"`
	NeedsYou struct {
		Count     int              `json:"count"`
		Items     []map[string]any `json:"items"`
		Truncated bool             `json:"truncated"`
	} `json:"needs_you"`
	Work struct {
		Missions []struct {
			MissionID    string         `json:"mission_id"`
			Status       string         `json:"status"`
			Reason       string         `json:"reason"`
			Since        string         `json:"since"`
			WaitingOn    string         `json:"waiting_on"`
			WaitingRoles []string       `json:"waiting_roles"`
			Slices       map[string]any `json:"slices"`
			NextSteps    []any          `json:"next_steps"`
			ETA          map[string]any `json:"eta"`
			Tasks        []struct {
				TaskID       string   `json:"task_id"`
				Status       string   `json:"status"`
				Reason       string   `json:"reason"`
				Since        string   `json:"since"`
				WaitingRoles []string `json:"waiting_roles"`
				UsualS       *int64   `json:"usual_s"`
				DependsOn    []string `json:"depends_on"`
				OffPlan      bool     `json:"off_plan"`
			} `json:"tasks"`
		} `json:"missions"`
		Truncated bool `json:"truncated"`
	} `json:"work"`
	Live   []map[string]any `json:"live"`
	Recent struct {
		Tasks          []map[string]any `json:"tasks"`
		FailedAttempts []map[string]any `json:"failed_attempts"`
	} `json:"recent"`
	Plan []struct {
		PlanNodeID string           `json:"plan_node_id"`
		Kind       string           `json:"kind"`
		Count      map[string]any   `json:"count"`
		Rows       []map[string]any `json:"rows"`
		Items      []struct {
			PlanNodeID  string         `json:"plan_node_id"`
			Count       map[string]any `json:"count"`
			ActiveSteps []string       `json:"active_steps"`
		} `json:"items"`
	} `json:"plan"`
	Hosts []map[string]any `json:"hosts"`
}

func (w *world) progress(t *testing.T) progressDoc {
	t.Helper()
	ctx := ctxOf(t)
	out, err := planstore.Progress(ctx, w.rt)
	ok(t, err)
	raw, err := json.Marshal(out)
	ok(t, err)
	var doc progressDoc
	ok(t, json.Unmarshal(raw, &doc))
	return doc
}

// Review 184 B4: a card is one item however many proposals name it, a card
// decided is no item, and the list stops at its bound and says so.
//
// Serial: it lowers needs_you's bound (LimitPendingChangesForTest).
func TestNeedsYouOneItemPerCard(t *testing.T) {
	w := newWorld(t)
	card := func(id string) func(context.Context, planstore.Card) (string, error) {
		return func(context.Context, planstore.Card) (string, error) { return id, nil }
	}
	for key, q := range map[string]string{"c1": "q_same", "c2": "q_same", "c3": "q_other", "c4": "q_done"} {
		_, err := planstore.ProposePlanChange(ctx, w.rt, wr("thread:s1"), key, root, "rename the item "+key,
			[]planstore.PlanChange{{Op: "retitle", Node: item, Title: key}}, card(q))
		ok(t, err)
	}
	refused := func(_ context.Context, id string) (seenfacts.Question, bool) {
		if id == "q_done" {
			return seenfacts.Question{ID: id, Status: seenfacts.QuestionAnswered, Option: planstore.CardRefuseKey, AnsweredBy: "owner",
				AnsweredAt: "2026-10-14T20:00:00Z"}, true
		}
		return seenfacts.Question{ID: id, Status: "open"}, true
	}
	if n, err := planstore.DecidePlanChanges(ctx, w.rt, refused, nil); err != nil || n != 1 {
		t.Fatalf("decided %d (%v); want the one answered card", n, err)
	}
	doc := w.progress(t)
	byID := map[any]map[string]any{}
	for _, it := range doc.NeedsYou.Items {
		byID[it["id"]] = it
	}
	if doc.NeedsYou.Count != 2 || len(byID) != 2 || doc.NeedsYou.Truncated {
		t.Fatalf("needs you %d %v (truncated %v); want one item per card", doc.NeedsYou.Count, doc.NeedsYou.Items, doc.NeedsYou.Truncated)
	}
	if refs, _ := byID["question:q_same"]["also_refs"].([]any); len(refs) != 1 {
		t.Errorf("the shared card's other proposals %v; want one", byID["question:q_same"]["also_refs"])
	}
	defer store.LimitPendingChangesForTest(1)()
	doc = w.progress(t)
	if doc.NeedsYou.Count != 1 || !doc.NeedsYou.Truncated {
		t.Fatalf("needs you at a bound of 1: %d, truncated %v", doc.NeedsYou.Count, doc.NeedsYou.Truncated)
	}
}
