package store_test

import (
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// closeReason is the reason of mission m's last move.
func (w *world) closeReason(t *testing.T, m string) string {
	t.Helper()
	var reason string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'reason' FROM agentrt.event WHERE mission_id = $1
	  AND event_type = 'mission_state_changed' ORDER BY event_id DESC LIMIT 1`, m).Scan(&reason))
	return reason
}

// PROD 2026-11-06: missions whose tasks were all finished stayed active
// until the coordinator stopped them by hand. A task withdrawn by its
// proposer, cancelled or rejected after its change reached main, or cancelled
// while a stop held it no longer holds its mission open; an implementation
// on main delivers what the goal asked for. A task still to do holds it.
func TestCloseMissionsFinished(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.legacy(t, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ('thread:s1', 'thread', 's1')`)
	for _, m := range []string{"M-3", "M-4", "M-5", "M-6", "M-7"} {
		w.goalMission(t, m)
	}
	// M-3: an accepted implementation and one its proposer withdrew.
	w.accept(t, w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"}))
	withdrawn := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	// M-4: its only implementation cancelled after it reached main.
	w.accept(t, w.ready(t, store.Task{MissionID: "M-4", Role: "plan", OffPlanReason: "a goal", Effect: "none"}))
	landed := w.ready(t, store.Task{MissionID: "M-4", OffPlanReason: "a goal"})
	// M-5: two tasks cancelled while a stop held them: one by its own stop,
	// since lifted; one by its mission's, still set.
	own := w.ready(t, store.Task{MissionID: "M-5", OffPlanReason: "a goal"})
	byMission := w.ready(t, store.Task{MissionID: "M-5", OffPlanReason: "a goal"})
	// M-6: accepted and withdrawn, but one task still ready.
	w.accept(t, w.ready(t, store.Task{MissionID: "M-6", OffPlanReason: "a goal"}))
	withdrawn6 := w.ready(t, store.Task{MissionID: "M-6", OffPlanReason: "a goal"})
	w.ready(t, store.Task{MissionID: "M-6", OffPlanReason: "a goal"})
	// M-7: a task the controller cancelled with no stop: it holds as before.
	w.accept(t, w.ready(t, store.Task{MissionID: "M-7", OffPlanReason: "a goal"}))
	cancelled := w.ready(t, store.Task{MissionID: "M-7", OffPlanReason: "a goal"})
	ownStop, missionStop := store.NewControlID(), store.NewControlID()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		for _, id := range []string{withdrawn, withdrawn6} {
			if err := store.MoveTask(ctx, tx, "thread:s1", id, "cancelled", "not needed any more"); err != nil {
				return err
			}
		}
		if _, err := store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-4"), Type: "note", Actor: "controller",
			MissionID: "M-4", TaskID: landed, Body: map[string]any{"text": "on main", "landed": commitA}}); err != nil {
			return err
		}
		if err := store.MoveTask(ctx, tx, "controller", landed, "cancelled", "its change is on main"); err != nil {
			return err
		}
		if err := store.SetControl(ctx, tx, "owner", ownStop, "task", own, "stop", "wrong branch"); err != nil {
			return err
		}
		if err := store.SetControl(ctx, tx, "owner", missionStop, "mission", "M-5", "stop", "the goal changed"); err != nil {
			return err
		}
		for _, id := range []string{own, byMission} {
			if err := store.MoveTask(ctx, tx, "controller", id, "cancelled", "stopped"); err != nil {
				return err
			}
		}
		return store.MoveTask(ctx, tx, "controller", cancelled, "cancelled", "superseded")
	}))
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.ClearControl(ctx, tx, "owner", ownStop) }))

	closed, err := store.CloseMissions(ctx, w.rt)
	if err != nil || !slices.Equal(closed, []string{"M-3", "M-4", "M-5"}) {
		t.Fatalf("closed %v %v", closed, err)
	}
	for m, want := range map[string]string{"M-3": "done", "M-4": "done", "M-5": "stopped", "M-6": "active", "M-7": "active"} {
		if got := w.state(t, "mission", m); got != want {
			t.Errorf("%s is %s, want %s", m, got, want)
		}
	}
	for m, want := range map[string]string{
		"M-3": "every task is accepted (1), withdrawn by its proposer (1)",
		"M-4": "every task is accepted (1), on main (1)",
		"M-5": "its remaining tasks were stopped with nothing left (2); nothing the goal asked for was accepted",
	} {
		if got := w.closeReason(t, m); got != want {
			t.Errorf("%s closed with %q, want %q", m, got, want)
		}
	}
}

// A stop set after a task was cancelled and since lifted never held it: the
// task holds its mission open.
func TestCloseMissionsLiftedStopAfterCancel(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	w.accept(t, w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"}))
	task := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveTask(ctx, tx, "controller", task, "cancelled", "superseded") }))
	stop := store.NewControlID()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.SetControl(ctx, tx, "owner", stop, "task", task, "stop", "late") }))
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.ClearControl(ctx, tx, "owner", stop) }))
	if closed, err := store.CloseMissions(ctx, w.rt); err != nil || len(closed) != 0 {
		t.Fatalf("closed %v %v", closed, err)
	}
}
