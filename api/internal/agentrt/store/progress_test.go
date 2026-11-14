package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// ended seeds a finished task of M-1 on s1 with its attempts, as the runtime
// leaves them: each attempt's rows, its computed and submitted events, a
// verification accepting the last attempt when the task is accepted, and the
// event that ended the task. outcomes are the attempts' conclusions, the
// last one deciding the task (done: accepted, failed: rejected).
func (w *world) ended(t *testing.T, outcomes ...string) (string, []string) {
	t.Helper()
	final := "accepted"
	if outcomes[len(outcomes)-1] == "failed" {
		final = "rejected"
	}
	return w.endedAs(t, final, outcomes...)
}

// endedAs is ended with the task's end given: a task rejected after an
// attempt that ended done (its verification failed) has no attempt
// accepted.
func (w *world) endedAs(t *testing.T, final string, outcomes ...string) (string, []string) {
	t.Helper()
	ctx := ctxOf(t)
	task := store.NewTaskID()
	var attempts []string
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: an ended task"}}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.task (task_id, mission_id, plan_node_id, role, task_class, title, spec_artifact_id,
		  effect_class, state, hard_limit_s, created_by_actor_id) VALUES ($1, 'M-1', $2, 'implement', 'go', 'ended', $3, 'branch', $4, 3600, 'controller')`,
			task, s1, w.spec, final); err != nil {
			return err
		}
		legacy := func() error {
			_, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
				Body: map[string]any{"source": "test: an ended task"}})
			return err
		}
		for i, outcome := range outcomes {
			a := store.NewAttemptID()
			attempts = append(attempts, a)
			if err := legacy(); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO agentrt.attempt (attempt_id, task_id, attempt_no, executor_actor_id, lease_epoch,
			  lease_expires_at, heartbeat_at, state, started_at, computed_at, persisted_at, ended_at, conclusion)
			  VALUES ($1, $2, $3, 'exec:codex', nextval('agentrt.lease_epoch_seq'), now(), now(), 'finished',
			    clock_timestamp() - interval '100 seconds', clock_timestamp(), clock_timestamp(), clock_timestamp(), $4)`, a, task, i+1, outcome); err != nil {
				return err
			}
			for _, ev := range []store.Event{
				{Type: "attempt_computed", Body: map[string]any{"end_how": "exit", "conclusion": outcome}},
				{Type: "attempt_submitted", Body: map[string]any{}},
			} {
				ev.Stream, ev.Actor, ev.MissionID, ev.TaskID, ev.AttemptID = "mission:M-1", "controller", "M-1", task, a
				if _, err := store.Append(ctx, tx, ev); err != nil {
					return err
				}
			}
		}
		if final == "accepted" {
			if err := legacy(); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO agentrt.verification (verification_id, task_id, attempt_id, method, verdict,
			  evidence_artifact_id, accepts_attempt) VALUES ($1, $2, $3, 'deterministic', 'pass', $4, true)`,
				store.NewVerificationID(), task, attempts[len(attempts)-1], w.spec); err != nil {
				return err
			}
		}
		_, err := store.Append(ctx, tx, store.Event{Stream: "mission:M-1", Type: "task_state_changed", Actor: "controller", MissionID: "M-1",
			TaskID: task, Body: map[string]any{"from": "verifying", "to": final}})
		return err
	}))
	return task, attempts
}

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
	out, err := store.Progress(ctx, w.rt)
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
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-1", "active", "test") }))
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

// Live attempts in start order by time, not by the text of the time (a
// fraction of a second sorts after the whole second).
func TestLiveOrder(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 10, 14, 20, 0, 0, 0, time.UTC)
	// The ids sort the other way round from the times; as text,
	// "…00.1Z" sorts before "…00Z".
	got := store.SortLiveForTest([]*store.ProgressAttempt{
		store.LiveForTest("a_a_late", "running", true, nil, nil, base.Add(100*time.Millisecond)),
		store.LiveForTest("a_b_first", "running", true, nil, nil, base),
		store.LiveForTest("a_0_none", "leased", true, nil, nil, time.Time{}),
	})
	if fmt.Sprint(got) != "[a_b_first a_a_late a_0_none]" {
		t.Fatalf("order %v", got)
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
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-1", "active", "test") }))
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		return store.MoveMission(ctx, tx, "controller", "M-1", "paused", "the Owner asked")
	}))
	for _, m := range w.progress(t).Work.Missions {
		if m.MissionID == "M-1" && (m.Status != store.StatusBlocked || m.Reason != "the mission is paused by controller: the Owner asked" ||
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
		_, err := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: s4, Parent: item, Kind: "step", Label: "2.1-4", Title: "s4",
			MissionID: "M-2", StepKind: "agent", Ordinal: 3, Root: root})
		return err
	}))
	w.ready(t, store.Task{})
	w.ready(t, store.Task{})
	_, err := w.task(t, "M-2", s4, "", "implement")
	ok(t, err)
	w.ended(t, "done") // an ended task on s1 holds nothing up
	for name, c := range map[string]struct {
		changes []store.PlanChange
		want    string
	}{
		"retitle an item: the tasks on its steps": {[]store.PlanChange{{Op: "retitle", Node: item, Title: "x"}}, "map[missions:[M-1 M-2] unfinished_tasks:0]"},
		"retitle a step": {[]store.PlanChange{{Op: "retitle", Node: s1, Title: "x"}}, "map[missions:[M-1] unfinished_tasks:0]"},
		"a mission_id a retitle carries binds nothing": {[]store.PlanChange{{Op: "retitle", Node: s1, Title: "x", MissionID: "M-2"}}, "map[missions:[M-1] unfinished_tasks:0]"},
		"rebind a step: both missions":                 {[]store.PlanChange{{Op: "rebind_step", Node: s1, MissionID: "M-2"}}, "map[missions:[M-1 M-2] unfinished_tasks:0]"},
		"bind a step with no mission":                  {[]store.PlanChange{{Op: "rebind_step", Node: s2, MissionID: "M-2"}}, "map[missions:[M-2] unfinished_tasks:0]"},
		"add a step to an item":                        {[]store.PlanChange{{Op: "add_node", Parent: item, Kind: "step", Title: "x"}}, "map[missions:[M-1 M-2] unfinished_tasks:0]"},
		"add an item to a round: nothing known":        {[]store.PlanChange{{Op: "add_node", Parent: root, Kind: "item", Title: "x"}}, "map[unknown:the change touches no step a mission's work sits on]"},
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
	card := func(id string) func(context.Context, store.Card) (string, error) {
		return func(context.Context, store.Card) (string, error) { return id, nil }
	}
	for key, q := range map[string]string{"c1": "q_same", "c2": "q_same", "c3": "q_other", "c4": "q_done"} {
		_, err := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), key, root, "rename the item "+key,
			[]store.PlanChange{{Op: "retitle", Node: item, Title: key}}, card(q))
		ok(t, err)
	}
	refused := func(_ context.Context, id string) (store.Question, bool) {
		if id == "q_done" {
			return store.Question{ID: id, Status: store.QuestionAnswered, Option: store.CardRefuseKey, AnsweredBy: "owner",
				AnsweredAt: "2026-10-14T20:00:00Z"}, true
		}
		return store.Question{ID: id, Status: "open"}, true
	}
	if n, err := store.DecidePlanChanges(ctx, w.rt, refused, nil); err != nil || n != 1 {
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
