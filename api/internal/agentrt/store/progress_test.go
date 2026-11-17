//bifrost:owner plan

package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/storekit"
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

// A mission without tasks: proposed waits for the Owner (with what for);
// active with steps not done is idle (the coordinator's turn).
func TestProgressMissionWithoutTasks(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return storekit.MoveMission(ctx, tx, "controller", "M-1", "active", "test") }))
	var lastM1, proposedM2 time.Time
	ok(t, w.rt.QueryRow(ctx, `SELECT max(at) FILTER (WHERE mission_id = 'M-1'),
	    max(at) FILTER (WHERE mission_id = 'M-2' AND event_type = 'mission_proposed') FROM agentrt.event`).Scan(&lastM1, &proposedM2))
	doc := w.progress(t)
	got := map[string]string{}
	since := map[string]string{}
	for _, m := range doc.Work.Missions {
		got[m.MissionID], since[m.MissionID] = m.Status+"/"+m.WaitingOn, m.Since
	}
	if got["M-1"] != "idle/coordinator" || got["M-2"] != "waiting_owner/owner" {
		t.Fatalf("missions %v", got)
	}
	// Idle since its last event; waiting since it was proposed.
	if !sameMoment(t, since["M-1"], lastM1) || !sameMoment(t, since["M-2"], proposedM2) {
		t.Fatalf("since %v; want M-1 %s, M-2 %s", since, lastM1, proposedM2)
	}
}

// sameMoment says a projection time is the database's time t.
func sameMoment(t *testing.T, got string, want time.Time) bool {
	t.Helper()
	at, err := time.Parse(time.RFC3339Nano, got)
	return err == nil && at.Equal(want)
}

// A paused mission without tasks is blocked by its pause, not idle.
func TestProgressPausedMissionWithoutTasks(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return storekit.MoveMission(ctx, tx, "controller", "M-1", "active", "test") }))
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		return storekit.MoveMission(ctx, tx, "controller", "M-1", "paused", "the Owner asked")
	}))
	for _, m := range w.progress(t).Work.Missions {
		if m.MissionID == "M-1" && (m.Status != planstore.StatusBlocked || m.Reason != "the mission is paused by controller: the Owner asked" ||
			m.WaitingOn != "coordinator" || m.Since == "") {
			t.Fatalf("M-1 %s %q %s since %q", m.Status, m.Reason, m.WaitingOn, m.Since)
		}
	}
}

// Review 184 B4: what a plan change holds up comes from one set: the steps it
// touches (an item's are the steps under it), the tasks it moves, and the
// missions of these and the ones it binds to.
func TestChangeBlocks(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	const s4 = "R2:2.1-4"
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := planstore.CreatePlanNode(ctx, tx, "controller", planstore.PlanNode{ID: s4, Parent: item, Kind: "step", Label: "2.1-4", Title: "s4",
			MissionID: "M-2", StepKind: "agent", Ordinal: 3, Root: root})
		return err
	}))
	for name, c := range map[string]struct {
		changes []planstore.PlanChange
		want    string
	}{
		"retitle an item: the tasks on its steps": {[]planstore.PlanChange{{Op: "retitle", Node: item, Title: "x"}}, "map[missions:[M-1 M-2] unfinished_tasks:0]"},
		"retitle a step": {[]planstore.PlanChange{{Op: "retitle", Node: s1, Title: "x"}}, "map[missions:[M-1] unfinished_tasks:0]"},
		"a mission_id a retitle carries binds nothing": {[]planstore.PlanChange{{Op: "retitle", Node: s1, Title: "x", MissionID: "M-2"}}, "map[missions:[M-1] unfinished_tasks:0]"},
		"rebind a step: both missions":                 {[]planstore.PlanChange{{Op: "rebind_step", Node: s1, MissionID: "M-2"}}, "map[missions:[M-1 M-2] unfinished_tasks:0]"},
		"bind a step with no mission":                  {[]planstore.PlanChange{{Op: "rebind_step", Node: s2, MissionID: "M-2"}}, "map[missions:[M-2] unfinished_tasks:0]"},
		"add a step to an item":                        {[]planstore.PlanChange{{Op: "add_node", Parent: item, Kind: "step", Title: "x"}}, "map[missions:[M-1 M-2] unfinished_tasks:0]"},
		"add an item to a round: nothing known":        {[]planstore.PlanChange{{Op: "add_node", Parent: root, Kind: "item", Title: "x"}}, "map[unknown:the change touches no step a mission's work sits on]"},
	} {
		got, err := store.ChangeBlocksForTest(ctx, w.rt, c.changes)
		ok(t, err)
		if fmt.Sprint(got) != c.want {
			t.Errorf("%s: %v; want %s", name, got, c.want)
		}
	}
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
