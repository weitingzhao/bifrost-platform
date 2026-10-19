package store_test

import (
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// accept moves a task through to accepted, as the kernel and acceptance do.
func (w *world) accept(t *testing.T, id string) {
	t.Helper()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		for _, to := range []string{"leased", "running", "submitted", "accepted"} {
			if err := store.MoveTaskForTest(ctx, tx, "controller", id, to); err != nil {
				return err
			}
		}
		return nil
	}))
}

// goalMission is an active mission bound to no plan step, as a goal makes.
func (w *world) goalMission(t *testing.T, id string) {
	t.Helper()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.CreateMission(ctx, tx, "controller", store.Mission{ID: id, Title: "goal", Goal: "a goal",
			Deadline: time.Now().Add(24 * time.Hour), Repos: []string{"bifrost-platform"}, Success: []string{store.GoalSuccess}}); err != nil {
			return err
		}
		return store.MoveMission(ctx, tx, "controller", id, "active", "test")
	}))
}

// A goal's mission closes once every task is accepted with an
// implementation among them; not before, not when a task is not accepted,
// not when it is bound to plan steps, and only once.
func TestCloseMissions(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	for _, m := range []string{"M-3", "M-4", "M-5", "M-6", "M-7"} {
		w.goalMission(t, m)
	}
	// M-7: every task accepted, but paused: only an active mission closes.
	w.accept(t, w.ready(t, store.Task{MissionID: "M-7", OffPlanReason: "a goal"}))
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-7", "paused", "test") }))
	// M-3: plan, implementation, its verification and review, all accepted.
	w.accept(t, w.ready(t, store.Task{MissionID: "M-3", Role: "plan", OffPlanReason: "a goal", Effect: "none"}))
	impl := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	w.accept(t, impl)
	for _, role := range []string{"verify", "review"} {
		w.accept(t, w.ready(t, store.Task{MissionID: "M-3", Role: role, Subject: impl, OffPlanReason: "a goal", Effect: "none"}))
	}
	// M-4: the implementation is still running.
	w.accept(t, w.ready(t, store.Task{MissionID: "M-4", Role: "plan", OffPlanReason: "a goal", Effect: "none"}))
	open := w.ready(t, store.Task{MissionID: "M-4", OffPlanReason: "a goal"})
	// M-5: only a planning task, accepted (its plan proposed nothing yet).
	w.accept(t, w.ready(t, store.Task{MissionID: "M-5", Role: "plan", OffPlanReason: "a goal", Effect: "none"}))
	// M-6: an implementation blocked.
	blocked := w.ready(t, store.Task{MissionID: "M-6", OffPlanReason: "a goal"})
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveTaskForTest(ctx, tx, "controller", blocked, "blocked") }))
	// M-1 is bound to plan steps: its tasks accepted, it stays active.
	w.accept(t, w.ready(t, store.Task{}))

	closed, err := store.CloseMissions(ctx, w.rt)
	if err != nil || !slices.Equal(closed, []string{"M-3"}) {
		t.Fatalf("closed %v %v", closed, err)
	}
	for m, want := range map[string]string{"M-1": "active", "M-3": "done", "M-4": "active", "M-5": "active", "M-6": "active", "M-7": "paused"} {
		if got := w.state(t, "mission", m); got != want {
			t.Errorf("%s is %s, want %s", m, got, want)
		}
	}
	var reason, actor string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'reason', actor_id FROM agentrt.event WHERE mission_id = 'M-3'
	  AND event_type = 'mission_state_changed' ORDER BY event_id DESC LIMIT 1`).Scan(&reason, &actor))
	if reason != "every task is accepted (4)" || actor != "controller" {
		t.Fatalf("the closing event: %q by %s", reason, actor)
	}
	if again, err := store.CloseMissions(ctx, w.rt); err != nil || len(again) != 0 {
		t.Fatalf("a second pass closed %v %v", again, err)
	}
	// M-4's implementation accepted: it closes on the next pass.
	w.accept(t, open)
	if closed, err = store.CloseMissions(ctx, w.rt); err != nil || !slices.Equal(closed, []string{"M-4"}) {
		t.Fatalf("after M-4's last task: %v %v", closed, err)
	}
}
