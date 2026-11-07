package store_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// landedNote writes a landed note on a task, as RecordLandings does, with
// landed_via when via is set.
func (w *world) landedNote(t *testing.T, mission, task, sha, via string) {
	t.Helper()
	body := map[string]any{"text": "its change is on main", "landed": sha}
	if via != "" {
		body["landed_via"] = via
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.Append(ctx, tx, store.Event{Stream: store.MissionStream(mission), Type: "note", Actor: "controller",
			MissionID: mission, TaskID: task, Body: body})
		return err
	}))
}

// PROD 2026-11-06: some thirty tasks whose change was on main still read
// ready or verifying. An implementation that has not ended and has a landed
// note is accepted by the pass, whatever its state (ready, verifying with an
// open check, blocked with an open card, running under a stop on the task),
// its live attempt and its open check cancelled; the check counts as a
// closed one (its reason begins as CloseStoppedChecks' do), and the blocked
// task's card is withdrawn. A task without a note, and one already
// cancelled, are left as they are; a second pass changes nothing.
func TestCloseOnMain(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	goal := store.Task{MissionID: "M-3", OffPlanReason: "a goal"}
	stopped := w.ready(t, goal)
	run := w.claimed(t, stopped, "h1", cursor, true)
	w.control(t, "c-stop", "task", stopped, "stop")
	verifying := w.ready(t, goal)
	w.moveThrough(t, verifying, "leased", "running", "submitted", "verifying")
	check := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal", Role: "verify", Subject: verifying, Effect: "none"})
	ready := w.ready(t, goal)
	blocked := w.ready(t, goal)
	w.moveThrough(t, blocked, "blocked")
	cancelled := w.ready(t, goal)
	w.moveThrough(t, cancelled, "cancelled")
	unlanded := w.ready(t, goal)
	a := &asker{}
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 || a.cards[0].TaskID != blocked {
		t.Fatalf("asked %d (%v); want the blocked task's card", n, err)
	}
	q := a.lastID()
	for _, id := range []string{stopped, ready, blocked, cancelled} {
		w.landedNote(t, "M-3", id, commitB, "")
	}
	w.landedNote(t, "M-3", verifying, commitB, store.LandedViaHarvest)
	cancelledMove := w.lastMove(t, cancelled)

	if n, err := store.CloseOnMain(ctx, w.rt); err != nil || n != 4 {
		t.Fatalf("closed %d (%v); want 4", n, err)
	}
	for id, want := range map[string]string{
		stopped:   "its change is on main of bifrost-platform as " + commitB[:12] + " (on main)",
		ready:     "its change is on main of bifrost-platform as " + commitB[:12] + " (on main)",
		blocked:   "its change is on main of bifrost-platform as " + commitB[:12] + " (on main)",
		verifying: "its change is on main of bifrost-platform as " + commitB[:12] + " (" + store.LandedViaHarvest + ")",
	} {
		if s := w.state(t, "task", id); s != "accepted" {
			t.Errorf("task %s is %s, want accepted", id, s)
		}
		if r := w.lastMove(t, id); r != want {
			t.Errorf("task %s moved because %q, want %q", id, r, want)
		}
	}
	if s := w.state(t, "attempt", run.AttemptID); s != "cancelled" || w.events(t, "attempt_cancelled", run.AttemptID) != 1 {
		t.Errorf("the stopped task's attempt is %s", s)
	}
	if _, err := store.Heartbeat(ctx, w.rt, fence(run, "h1")); !errors.Is(err, store.ErrFenced) {
		t.Errorf("the heartbeat of the cancelled attempt: %v; want fenced", err)
	}
	if s, r := w.state(t, "task", check), w.lastMove(t, check); s != "cancelled" || !strings.HasPrefix(r, "its subject task "+verifying+" ") {
		t.Errorf("the open check is %s, moved because %q", s, r)
	}
	if s := w.state(t, "task", unlanded); s != "ready" {
		t.Errorf("the task without a landed note is %s", s)
	}
	if s, r := w.state(t, "task", cancelled), w.lastMove(t, cancelled); s != "cancelled" || r != cancelledMove {
		t.Errorf("the cancelled task is %s, moved because %q", s, r)
	}
	var wd withdrawals
	if n, err := store.WithdrawStaleCards(ctx, w.rt, a.question, wd.withdraw); err != nil || n != 1 || !slices.Equal(wd.ids, []string{q}) ||
		!strings.HasPrefix(wd.why[0], "task "+blocked+" is accepted: its change is on main") {
		t.Fatalf("withdrew %d (%v): %v %v", n, err, wd.ids, wd.why)
	}

	moves := w.taskEvents(t, "task_state_changed", unlanded)
	if n, err := store.CloseOnMain(ctx, w.rt); err != nil || n != 0 {
		t.Fatalf("a second pass closed %d (%v)", n, err)
	}
	if w.taskEvents(t, "task_state_changed", unlanded) != moves || w.events(t, "attempt_cancelled", run.AttemptID) != 1 {
		t.Fatal("a second pass moved something")
	}
	w.chainHolds(t, "mission:M-3")
}
