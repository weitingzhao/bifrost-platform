package store_test

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
)

// stopped is the reason a card of a task a stop holds is withdrawn with.
func stopped(task, reason string) string {
	return "task " + task + " is stopped (" + reason + "); only the Owner lifts a stop, and a new card is asked if it is still blocked then"
}

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

// A card asked before its task was stopped, by the task's own stop or its
// mission's, is withdrawn with the stop's reason; DecideBlocked then closes
// it as withdrawn and moves nothing; no card is asked while the stop holds,
// and a new one, card 2 of the block, once it is lifted.
func TestStoppedTaskCardIsWithdrawn(t *testing.T) {
	t.Parallel()
	for _, scope := range []string{"task", "mission"} {
		t.Run(scope, func(t *testing.T) {
			t.Parallel()
			w := newKernelWorld(t)
			w.goalMission(t, "M-3")
			id := w.blockedIn(t, "M-3")
			kept := w.blockedIn(t, "M-3") // its card is withdrawn only by its mission's stop
			a := &asker{}
			if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 2 {
				t.Fatalf("asked %d (%v)", n, err)
			}
			q := map[string]string{a.cards[0].TaskID: "q_a", a.cards[1].TaskID: "q_b"}
			ref := id
			if scope == "mission" {
				ref = "M-3"
			}
			w.stop(t, "c1", scope, ref, "hold\n  it")

			var wd withdrawals
			n, err := store.WithdrawStaleCards(ctx, w.rt, a.question, wd.withdraw)
			ok(t, err)
			want := map[string]string{q[id]: stopped(id, "hold it")}
			if scope == "mission" {
				want[q[kept]] = stopped(kept, "hold it")
			}
			if n != len(want) || len(wd.ids) != len(want) {
				t.Fatalf("withdrew %d: %v %v", n, wd.ids, wd.why)
			}
			for i, qid := range wd.ids {
				if want[qid] != wd.why[i] {
					t.Fatalf("withdrew %s because %q; want %q", qid, wd.why[i], want[qid])
				}
				a.answer(qid, "withdrawn", "")
			}
			if n, err := store.DecideBlocked(ctx, w.rt, a.question, a.ack); err != nil || n != len(want) {
				t.Fatalf("decided %d (%v)", n, err)
			}
			if o, _, _ := w.closedCard(t, q[id]); o != "withdrawn" || w.state(t, "task", id) != "blocked" {
				t.Fatalf("the card closed %q; the task is %s", o, w.state(t, "task", id))
			}
			if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 0 {
				t.Fatalf("asked %d (%v) while the stop holds", n, err)
			}

			w.lift(t, "c1")
			if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != len(want) {
				t.Fatalf("after the lift: asked %d (%v)", n, err)
			}
			last := a.cards[len(a.cards)-1]
			if scope == "task" && (last.TaskID != id || !strings.Contains(last.Body, "card 2 for this block")) {
				t.Fatalf("after the lift: %s %q", last.TaskID, last.Body)
			}
			wd.ids = nil
			if n, err := store.WithdrawStaleCards(ctx, w.rt, a.question, wd.withdraw); err != nil || n != 0 {
				t.Fatalf("withdrew %d (%v) %v after the lift", n, err, wd.ids)
			}
		})
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

// A check whose subject is stopped is not itself stopped: it keeps its card
// (the budget plan M-328 handles such cards), and no withdrawal takes it.
func TestStoppedTaskCardOfACheckWhoseSubjectIsStopped(t *testing.T) {
	t.Parallel()
	w, _, impl, check := spentEnvelope(t)
	if n, err := store.BlockSpentForTest(ctx, w.rt); err != nil || n != 1 || w.state(t, "task", check) != "blocked" {
		t.Fatalf("blocked %d (%v); the check is %s", n, err, w.state(t, "task", check))
	}
	w.stop(t, "c1", "task", impl, "hold the work")
	a := &asker{}
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 || a.cards[0].TaskID != check {
		t.Fatalf("asked %d (%v) %v", n, err, a.cards)
	}
	var wd withdrawals
	if n, err := store.WithdrawStaleCards(ctx, w.rt, a.question, wd.withdraw); err != nil || n != 0 {
		t.Fatalf("withdrew %d (%v) %v", n, err, wd.why)
	}
}

// A blocked task that ends while its card is open, cancelled by the thread
// that proposed it, has its card withdrawn with the reason of the cancel, cut
// to what the question store takes; the card then closes as withdrawn.
func TestEndedTaskCardIsWithdrawn(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	w.legacy(t, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ('thread:crew-2', 'thread', 'crew 2')`)
	short := w.blockedIn(t, "M-3")
	long := w.blockedIn(t, "M-3")
	a := &asker{}
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 2 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	q := map[string]string{a.cards[0].TaskID: "q_a", a.cards[1].TaskID: "q_b"}
	why := map[string]string{short: "withdrawn by its proposer: the goal changed", long: "withdrawn: " + strings.Repeat("ever so long ", 60)}
	for _, id := range []string{short, long} {
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			return store.MoveTask(ctx, tx, "thread:crew-2", id, "cancelled", why[id])
		}))
	}
	var wd withdrawals
	if n, err := store.WithdrawStaleCards(ctx, w.rt, a.question, wd.withdraw); err != nil || n != 2 {
		t.Fatalf("withdrew %d (%v) %v", n, err, wd.why)
	}
	for i, qid := range wd.ids {
		reason := wd.why[i]
		switch qid {
		case q[short]:
			if reason != "task "+short+" is cancelled: withdrawn by its proposer: the goal changed" {
				t.Errorf("withdrew %s because %q", qid, reason)
			}
		case q[long]:
			if !strings.HasPrefix(reason, "task "+long+" is cancelled: withdrawn: ever so long") || !strings.HasSuffix(reason, "…") ||
				questions.BodyBytes(reason) > questions.ReasonLimit {
				t.Errorf("withdrew %s because %q (%d bytes)", qid, reason, questions.BodyBytes(reason))
			}
		default:
			t.Errorf("withdrew %s", qid)
		}
		a.answer(qid, "withdrawn", "")
	}
	if n, err := store.DecideBlocked(ctx, w.rt, a.question, a.ack); err != nil || n != 2 {
		t.Fatalf("decided %d (%v)", n, err)
	}
	for _, id := range []string{short, long} {
		if o, _, _ := w.closedCard(t, q[id]); o != "withdrawn" || w.state(t, "task", id) != "cancelled" {
			t.Fatalf("task %s: the card closed %q; the task is %s", id, o, w.state(t, "task", id))
		}
	}
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 0 {
		t.Fatalf("asked %d (%v) for ended tasks", n, err)
	}
}
