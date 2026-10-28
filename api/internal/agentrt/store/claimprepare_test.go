package store_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// TD-449: no controller pass runs between Ended and the next claim. The
// fourth claim must commit the block, not roll it back with its savepoint.
func TestClaimBlocksPreparationFailuresWithoutControllerPass(t *testing.T) {
	t.Parallel()
	w := newWorldOf(t, cursor)
	w.goalMission(t, "M-3")
	task := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	for round := 0; round < 2; round++ {
		for i := 1; i <= 3; i++ {
			c := w.prepFail(t, task, cursor, "# setup failed")
			if want := round*3 + i; c.AttemptNo != want {
				t.Fatalf("attempt %d; want %d", c.AttemptNo, want)
			}
		}
		if s := w.state(t, "task", task); s != "ready" {
			t.Fatalf("before claim: task is %s; want ready", s)
		}
		// Repeating the poll must neither lease nor write a second block.
		before := w.taskEvents(t, "task_state_changed", task)
		for range 2 {
			if c := w.claim(t, "h1", store.LeaseTTL, cursor); c != nil {
				t.Fatalf("leased after three preparation failures: %+v", c)
			}
		}
		if s := w.state(t, "task", task); s != "blocked" {
			t.Fatalf("after claim: task is %s; want blocked", s)
		}
		if got := w.taskEvents(t, "task_state_changed", task); got != before+1 {
			t.Fatalf("claim wrote %d state changes; want one block", got-before)
		}
		var attempts int
		ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.attempt WHERE task_id = $1`, task).Scan(&attempts))
		if want := (round + 1) * 3; attempts != want {
			t.Fatalf("attempt count %d; want %d", attempts, want)
		}
		want := fmt.Sprintf("its preparation failed before any executor started: attempts %d, %d, %d in a row",
			round*3+1, round*3+2, round*3+3)
		if reason := w.blockReason(t, task); !strings.HasPrefix(reason, want) {
			t.Fatalf("block reason %q; want prefix %q", reason, want)
		}
		if round == 0 {
			a := &asker{}
			if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 {
				t.Fatalf("asked %d (%v); want one preparation card", n, err)
			}
			if c := a.cards[0]; c.Recommended != 0 || !strings.Contains(c.Body, "Why: "+want) {
				t.Fatalf("preparation card: %+v", c)
			}
			a.answer(a.lastID(), store.QuestionAnswered, store.CardApplyKey)
			if n, err := store.DecideBlocked(ctx, w.rt, a.question, a.ack); err != nil || n != 1 {
				t.Fatalf("one more round decided %d (%v)", n, err)
			}
			if s := w.state(t, "task", task); s != "ready" {
				t.Fatalf("after one more round: task is %s; want ready", s)
			}
		}
	}
	w.chainHolds(t, "mission:M-3")
}

func TestClaimAllowsPreparationRunBrokenByExecutorFailure(t *testing.T) {
	t.Parallel()
	w := newWorldOf(t, cursor, codex)
	task := w.ready(t, store.Task{})
	for range 2 {
		w.prepFail(t, task, cursor, "")
	}
	c := w.claimed(t, task, "h1", cursor, false)
	w.fail(t, c, "h1")
	// Another executor can take it immediately, without waiting out cursor's
	// ordinary failure backoff; the preparation run itself no longer holds.
	c = w.claimed(t, task, "h2", codex, false)
	if c.AttemptNo != 4 {
		t.Fatalf("attempt %d; want 4", c.AttemptNo)
	}
}

func TestClaimAfterPreparationBlockTakesNextMission(t *testing.T) {
	t.Parallel()
	w := newWorldOf(t, cursor)
	task := w.ready(t, store.Task{})
	for range 3 {
		w.prepFail(t, task, cursor, "")
	}
	w.goalMission(t, "M-3")
	next := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "another goal"})
	// Commit the block and release that mission's locks before considering
	// another mission. The next host poll can lease the remaining task.
	if c := w.claim(t, "h1", store.LeaseTTL, cursor); c != nil {
		t.Fatalf("the blocking poll leased an attempt: %+v", c)
	}
	if s := w.state(t, "task", task); s != "blocked" {
		t.Fatalf("first task is %s; want blocked", s)
	}
	w.claimed(t, next, "h1", cursor, false)
}
