package planstore_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/storekit"
)

type ledgerProgressDoc struct {
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

func (w *world) progress(t *testing.T) ledgerProgressDoc {
	t.Helper()
	ctx := ctxOf(t)
	out, err := planstore.Progress(ctx, w.rt)
	ok(t, err)
	raw, err := json.Marshal(out)
	ok(t, err)
	var doc ledgerProgressDoc
	ok(t, json.Unmarshal(raw, &doc))
	return doc
}

// A mission without tasks: proposed waits for the Owner (with what for);
// active with steps not done is idle (the coordinator's turn).
//
// Origin: moved from store/progress_test.go (TestProgressMissionWithoutTasks), 2026-11-18 (TD-582).
func TestProgressMissionWithoutTasks(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error { return storekit.MoveMission(ctx, tx, "controller", "M-1", "active", "test") }))
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
//
// Origin: moved from store/progress_test.go (TestProgressPausedMissionWithoutTasks), 2026-11-18 (TD-582).
func TestProgressPausedMissionWithoutTasks(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error { return storekit.MoveMission(ctx, tx, "controller", "M-1", "active", "test") }))
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		return storekit.MoveMission(ctx, tx, "controller", "M-1", "paused", "the Owner asked")
	}))
	for _, m := range w.progress(t).Work.Missions {
		if m.MissionID == "M-1" && (m.Status != planstore.StatusBlockedForTest || m.Reason != "the mission is paused by controller: the Owner asked" ||
			m.WaitingOn != "coordinator" || m.Since == "") {
			t.Fatalf("M-1 %s %q %s since %q", m.Status, m.Reason, m.WaitingOn, m.Since)
		}
	}
}

// Review 184 B4: what a plan change holds up comes from one set: the steps it
// touches (an item's are the steps under it), the tasks it moves, and the
// missions of these and the ones it binds to.
//
// Origin: moved from store/progress_test.go (TestChangeBlocks), 2026-11-18 (TD-582).
func TestChangeBlocks(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	const s4 = "R2:2.1-4"
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := planstore.CreatePlanNode(ctx, tx, "controller", planstore.PlanNode{ID: s4, Parent: item, Kind: "step", Label: "2.1-4", Title: "s4",
			MissionID: "M-2", StepKind: "agent", Ordinal: 3, Root: root})
		return err
	}))
	for name, c := range map[string]struct {
		changes []planstore.PlanChange
		want    string
	}{
		"retitle an item: the tasks on its steps": {[]planstore.PlanChange{{Op: "retitle", Node: item, Title: "x"}}, "map[missions:[M-1 M-2]]"},
		"retitle a step": {[]planstore.PlanChange{{Op: "retitle", Node: s1, Title: "x"}}, "map[missions:[M-1]]"},
		"a mission_id a retitle carries binds nothing": {[]planstore.PlanChange{{Op: "retitle", Node: s1, Title: "x", MissionID: "M-2"}}, "map[missions:[M-1]]"},
		"rebind a step: both missions":                 {[]planstore.PlanChange{{Op: "rebind_step", Node: s1, MissionID: "M-2"}}, "map[missions:[M-1 M-2]]"},
		"bind a step with no mission":                  {[]planstore.PlanChange{{Op: "rebind_step", Node: s2, MissionID: "M-2"}}, "map[missions:[M-2]]"},
		"add a step to an item":                        {[]planstore.PlanChange{{Op: "add_node", Parent: item, Kind: "step", Title: "x"}}, "map[missions:[M-1 M-2]]"},
		"add an item to a round: nothing known":        {[]planstore.PlanChange{{Op: "add_node", Parent: root, Kind: "item", Title: "x"}}, "map[unknown:the change touches no step a mission's work sits on]"},
	} {
		got, err := changeBlocks(ctx, w.rt, c.changes)
		ok(t, err)
		if fmt.Sprint(got) != c.want {
			t.Errorf("%s: %v; want %s", name, got, c.want)
		}
	}
}

// changeBlocks is what changes would hold up, read in a transaction of their
// own.
func changeBlocks(ctx context.Context, db eventlog.DB, changes []planstore.PlanChange) (any, error) {
	var out any
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		var err error
		out, err = planstore.ChangeBlocksForTest(ctx, tx, changes)
		return err
	})
	return out, err
}
