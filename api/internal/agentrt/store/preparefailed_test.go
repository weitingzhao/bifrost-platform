package store_test

import (
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// prepFail leases task to tl on h1 and ends the attempt as agentd ends one
// whose preparation failed (agentd prepare.go): started, a checkpoint naming
// its report, computed and ended prepare_failed.
func (w *world) prepFail(t *testing.T, task string, tl store.Tool, report string) *store.Claim {
	t.Helper()
	c := w.claim(t, "h1", store.LeaseTTL, tl)
	if c == nil || c.TaskID != task {
		t.Fatalf("%s was not leased the task at once: %+v", tl.Executor, c)
	}
	f := fence(c, "h1")
	ok(t, store.Start(ctx, w.rt, f, "", ""))
	_, err := store.Checkpoint(ctx, w.rt, f, 1, []string{w.artifact(t, "report", report)}, false)
	ok(t, err)
	exit := 3
	_, err = store.Computed(ctx, w.rt, f, &exit, "prepare_failed", "failed")
	ok(t, err)
	_, err = store.Ended(ctx, w.rt, c.AttemptID, "h1", "prepare_failed", true)
	ok(t, err)
	return c
}

// TD-394 B (the Owner 2026-10-20 21:3x; PROD M-22: a setup broken on the
// host withdrew codex, cursor and claude three times each): a failure before
// the executor started is not the executor's. It waits for no backoff and
// withdraws no one; three in a row block the task with a card of their own,
// which quotes the last one's report and recommends nothing (fix the host,
// then give it one more round). One more round tries it three times more; a
// run broken by a failure of the executor's own blocks nothing.
func TestPreparationFailuresBlockWithTheirOwnCard(t *testing.T) {
	t.Parallel()
	w := newWorldOf(t, cursor)
	w.goalMission(t, "M-3")
	task := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	for i := 1; i <= 3; i++ {
		w.prepFail(t, task, cursor, "# The attempt failed before its executor started\n\nThe task's setup failed: npm ci exited 1 (registry down "+
			strings.Repeat("x", i)+")")
	}
	if n, err := store.BlockWithdrawn(ctx, w.rt); err != nil || n != 1 || w.state(t, "task", task) != "blocked" {
		t.Fatalf("blocked %d (%v), the task %s", n, err, w.state(t, "task", task))
	}
	reason := w.blockReason(t, task)
	if !strings.HasPrefix(reason, "its preparation failed before any executor started: attempts 1, 2, 3 in a row") ||
		strings.Contains(reason, "no executor on its route") {
		t.Fatalf("the block's reason: %q", reason)
	}

	a := &asker{}
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	c := a.cards[0]
	if c.Recommended != 0 {
		t.Fatalf("the card recommends option %d: %s", c.Recommended, c.RecommendedWhy)
	}
	for _, want := range []string{"its preparation failed on the host (its setup, checkout or a sibling), not an executor: fix that, then give it one more round",
		"Why: its preparation failed before any executor started: attempts 1, 2, 3 in a row",
		"Last preparation: failed on attempt 3 (", "Its report:", "npm ci exited 1 (registry down xxx)",
		store.BlockedRetry + ": it goes back to ready as it is, and its preparation is tried 3 times more before it is blocked again"} {
		if !strings.Contains(c.Body, want) {
			t.Fatalf("the card lacks %q:\n%s", want, c.Body)
		}
	}

	// One more round: the count starts again.
	a.answer(a.lastID(), store.QuestionAnswered, store.CardApplyKey)
	if n, err := store.DecideBlocked(ctx, w.rt, a.question, a.ack); err != nil || n != 1 || w.state(t, "task", task) != "ready" {
		t.Fatalf("decided %d (%v), the task %s", n, err, w.state(t, "task", task))
	}
	if n, err := store.BlockWithdrawn(ctx, w.rt); err != nil || n != 0 {
		t.Fatalf("the pass after one more round blocked %d (%v)", n, err)
	}
	// Two more, broken by a failure of the executor's own: nothing blocks.
	w.prepFail(t, task, cursor, "# again")
	c2 := w.claim(t, "h1", store.LeaseTTL, cursor)
	if c2 == nil || c2.TaskID != task {
		t.Fatalf("cursor was not leased the task: %+v", c2)
	}
	w.fail(t, c2, "h1")
	w.ago(t, task, 10*time.Minute)
	w.prepFail(t, task, cursor, "# and again")
	if n, err := store.BlockWithdrawn(ctx, w.rt); err != nil || n != 0 || w.state(t, "task", task) != "ready" {
		t.Fatalf("a broken run blocked %d (%v), the task %s", n, err, w.state(t, "task", task))
	}
	// Two more of its preparation, three in a row since the executor's own:
	// blocked again.
	w.prepFail(t, task, cursor, "# once more")
	w.prepFail(t, task, cursor, "# and once more")
	if n, err := store.BlockWithdrawn(ctx, w.rt); err != nil || n != 1 {
		t.Fatalf("three in a row again blocked %d (%v)", n, err)
	}
	if reason = w.blockReason(t, task); !strings.Contains(reason, "attempts 6, 7, 8 in a row") {
		t.Fatalf("the second block's reason: %q", reason)
	}
}
