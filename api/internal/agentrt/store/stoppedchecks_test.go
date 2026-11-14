package store_test

import (
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// moveThrough moves a task through the given states, as the kernel does.
func (w *world) moveThrough(t *testing.T, id string, states ...string) {
	t.Helper()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		for _, to := range states {
			if err := store.MoveTaskForTest(ctx, tx, "controller", id, to); err != nil {
				return err
			}
		}
		return nil
	}))
}

// 11-06, M-308: two of three budget cards were for checks of tasks already
// stopped. A blocked subject's pending checks, one ready and one blocked
// (for budget, with its card open), are cancelled by the pass, once; the
// blocked one gets no card before, its open card is closed as moot and then
// withdrawn from the question store. A verifying subject's check and a
// running check of a blocked subject are left as they are.
func TestStoppedChecksAreClosed(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	goal := store.Task{MissionID: "M-3", OffPlanReason: "a goal"}
	check := func(role, subject string) string {
		return w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal", Role: role, Subject: subject, Effect: "none"})
	}
	impl := w.ready(t, goal)
	w.moveThrough(t, impl, "leased", "running", "submitted", "verifying")
	ready := check("verify", impl)
	carded := check("review", impl)
	w.moveThrough(t, carded, "blocked")
	a := &asker{}
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 || a.cards[0].TaskID != carded {
		t.Fatalf("asked %d (%v) while its subject verifies; want the check's card", n, err)
	}
	q := a.lastID()
	budget := check("verify", impl)
	w.moveThrough(t, budget, "blocked")
	w.moveThrough(t, impl, "blocked")

	// Another subject, verifying, keeps its check; a blocked one keeps its
	// running check.
	verifying := w.ready(t, goal)
	w.moveThrough(t, verifying, "leased", "running", "submitted", "verifying")
	kept := check("verify", verifying)
	stopped := w.ready(t, goal)
	running := check("verify", stopped)
	w.moveThrough(t, running, "leased", "running")
	w.moveThrough(t, stopped, "blocked")

	n, err := store.AskBlocked(ctx, w.rt, a.ask)
	ok(t, err)
	for _, c := range a.cards[1:] {
		if c.TaskID == budget || c.TaskID == carded {
			t.Fatalf("a card for check %s of a blocked task", c.TaskID)
		}
	}
	if n != 2 {
		t.Fatalf("asked %d; want the two blocked subjects' cards", n)
	}

	if n, err := store.CloseStoppedChecks(ctx, w.rt); err != nil || n != 3 {
		t.Fatalf("closed %d (%v); want 3", n, err)
	}
	for _, id := range []string{ready, carded, budget} {
		if s := w.state(t, "task", id); s != "cancelled" {
			t.Errorf("check %s is %s, want cancelled", id, s)
		}
		if r, want := w.lastMove(t, id), "its subject task "+impl+" is blocked; this check can no longer decide anything"; r != want {
			t.Errorf("check %s: %q, want %q", id, r, want)
		}
	}
	for id, want := range map[string]string{kept: "ready", running: "running", impl: "blocked", verifying: "verifying", stopped: "blocked"} {
		if s := w.state(t, "task", id); s != want {
			t.Errorf("task %s is %s, want %s", id, s, want)
		}
	}
	if o, _, _ := w.closedCard(t, q); o != store.OutcomeMoot {
		t.Fatalf("the check's card closed %q, want moot", o)
	}
	var actor string
	ok(t, w.rt.QueryRow(ctx, `SELECT actor_id FROM agentrt.event WHERE task_id = $1 AND event_type = 'task_state_changed'
	  ORDER BY event_id DESC LIMIT 1`, budget).Scan(&actor))
	if actor != "controller" {
		t.Fatalf("cancelled by %q", actor)
	}
	if n, err := store.CloseStoppedChecks(ctx, w.rt); err != nil || n != 0 {
		t.Fatalf("a second pass closed %d (%v)", n, err)
	}

	var wd withdrawals
	if n, err := store.WithdrawStaleCards(ctx, w.rt, a.question, wd.withdraw); err != nil || n != 1 || !slices.Equal(wd.ids, []string{q}) ||
		wd.why[0] != "it checks task "+impl+", which is blocked; the check was closed" {
		t.Fatalf("withdrew %d (%v): %v %v", n, err, wd.ids, wd.why)
	}
	a.answer(q, "withdrawn", "")
	if n, err := store.WithdrawStaleCards(ctx, w.rt, a.question, wd.withdraw); err != nil || n != 0 {
		t.Fatalf("withdrew %d (%v) once the store reads it withdrawn", n, err)
	}
	if n, err := store.DecideBlocked(ctx, w.rt, a.question, a.ack); err != nil || n != 0 {
		t.Fatalf("decided %d (%v) on a card already closed", n, err)
	}
	if n, err := store.BlockSpentForTest(ctx, w.rt); err != nil || n != 0 {
		t.Fatalf("blocked %d (%v)", n, err)
	}
}

// An open card of a task that is no longer blocked decides nothing and is
// withdrawn.
func TestStoppedChecksCardOfATaskNoLongerBlocked(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	impl := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	w.moveThrough(t, impl, "blocked")
	a := &asker{}
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	var wd withdrawals
	if n, err := store.WithdrawStaleCards(ctx, w.rt, a.question, wd.withdraw); err != nil || n != 0 {
		t.Fatalf("withdrew %d (%v) while its task is blocked", n, err)
	}
	w.moveThrough(t, impl, "ready")
	if n, err := store.WithdrawStaleCards(ctx, w.rt, a.question, wd.withdraw); err != nil || n != 1 ||
		wd.why[0] != "its task is no longer blocked; this card no longer decides anything" {
		t.Fatalf("withdrew %d (%v): %v", n, err, wd.why)
	}
}
