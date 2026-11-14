package store_test

import (
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// blockedIn is a new task of mission, blocked as failed work.
func (w *world) blockedIn(t *testing.T, mission string) string {
	t.Helper()
	id := w.ready(t, store.Task{MissionID: mission, OffPlanReason: "a goal"})
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.FailWorkForTest(ctx, tx, id) }))
	return id
}

// stop sets a stop of scope on ref, with reason.
func (w *world) stop(t *testing.T, id, scope, ref, reason string) {
	t.Helper()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.SetControl(ctx, tx, "owner", id, scope, ref, "stop", reason) }))
}

func (w *world) lift(t *testing.T, id string) {
	t.Helper()
	if out, err := store.LiftControl(ctx, w.rt, "owner", id); err != nil || out["cleared"] != true {
		t.Fatalf("lift %s: %v (%v)", id, out, err)
	}
}

// The coordinator, 2026-11-06: a blocked task a stop holds, its own or its
// mission's, gets no card while it holds; a pause, a stop of another task
// and a stop already lifted take nothing away. Once the stop is lifted, the
// task still blocked gets its card.
func TestStoppedTaskCardIsNotAsked(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	w.goalMission(t, "M-4")
	own := w.blockedIn(t, "M-3")
	ofMission := w.blockedIn(t, "M-4")
	paused := w.blockedIn(t, "M-3")
	lifted := w.blockedIn(t, "M-3")
	other := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	w.stop(t, "c1", "task", own, "the coordinator holds it")
	w.stop(t, "c2", "mission", "M-4", "the coordinator holds the mission")
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		return store.SetControl(ctx, tx, "owner", "c3", "task", paused, "pause", "paused only")
	}))
	w.stop(t, "c4", "task", lifted, "lifted at once")
	w.lift(t, "c4")
	w.stop(t, "c5", "task", other, "another task")

	a := &asker{}
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 2 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	for _, c := range a.cards {
		if c.TaskID == own || c.TaskID == ofMission {
			t.Fatalf("a stopped task got a card: %s", c.TaskID)
		}
	}
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 0 {
		t.Fatalf("asked again %d (%v)", n, err)
	}

	w.lift(t, "c1")
	w.lift(t, "c2")
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 2 {
		t.Fatalf("after the lifts: asked %d (%v)", n, err)
	}
	got := map[string]bool{a.cards[2].TaskID: true, a.cards[3].TaskID: true}
	if !got[own] || !got[ofMission] {
		t.Fatalf("after the lifts the cards are for %v", got)
	}
}

// Review of attempt 1, P1: a card answered after its task was stopped, by
// the task's own stop or its mission's, before it could be withdrawn, moves
// nothing, whichever option: the card closes as moot, the task stays
// blocked, the mission and its other work stay as they are. Once the stop
// is lifted the task gets a new card.
func TestStoppedTaskCardAnswerMovesNothing(t *testing.T) {
	t.Parallel()
	for _, scope := range []string{"task", "mission"} {
		for _, option := range []string{store.CardApplyKey, store.CardRefuseKey, store.CardCloseKey} {
			t.Run(scope+" "+option, func(t *testing.T) {
				t.Parallel()
				w := newKernelWorld(t)
				w.goalMission(t, "M-3")
				id := w.blockedIn(t, "M-3")
				other := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
				a := &asker{}
				if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 {
					t.Fatalf("asked %d (%v)", n, err)
				}
				ref := id
				if scope == "mission" {
					ref = "M-3"
				}
				w.stop(t, "c1", scope, ref, "hold it")
				a.answer("q_a", store.QuestionAnswered, option)
				moves := w.taskEvents(t, "task_state_changed", id)
				if n, err := store.DecideBlocked(ctx, w.rt, a.question, a.ack); err != nil || n != 1 {
					t.Fatalf("decided %d (%v)", n, err)
				}
				if o, ans, _ := w.closedCard(t, "q_a"); o != store.OutcomeMoot || ans != option {
					t.Fatalf("the card closed %q %q", o, ans)
				}
				if s := w.state(t, "task", id); s != "blocked" || w.taskEvents(t, "task_state_changed", id) != moves {
					t.Fatalf("the stopped task is %s", s)
				}
				if s, m := w.state(t, "task", other), w.state(t, "mission", "M-3"); s != "ready" || m != "active" {
					t.Fatalf("the other task is %s, the mission %s", s, m)
				}
				if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 0 {
					t.Fatalf("asked %d (%v) while the stop holds", n, err)
				}
				w.lift(t, "c1")
				if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 || a.cards[1].TaskID != id {
					t.Fatalf("after the lift: asked %d (%v)", n, err)
				}
			})
		}
	}
}
