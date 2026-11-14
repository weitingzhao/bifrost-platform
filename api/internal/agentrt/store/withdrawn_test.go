package store_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// newWorldOf is newKernelWorld with only the given tools as the jobs'
// candidates, so a test knows every executor a task's route has.
func newWorldOf(t *testing.T, tools ...store.Tool) *world {
	t.Helper()
	w := newWorld(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-1", "active", "test") }))
	for i := 1; i <= 3; i++ {
		ok(t, store.RegisterHost(ctx, w.rt, "owner", fmt.Sprintf("h%d", i), "dev"))
	}
	_, err := w.rt.Exec(ctx, agentrttest.DispatchRules)
	ok(t, err)
	w.legacy(t, agentrttest.OwnerFactsRule, agentrttest.BindOwnerFacts)
	w.jobs(t, "active", "*", tools...)
	return w
}

// blockReason is the reason of the task's latest move to blocked.
func (w *world) blockReason(t *testing.T, task string) string {
	t.Helper()
	var reason string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'reason' FROM agentrt.event WHERE task_id = $1 AND event_type = 'task_state_changed'
	  AND body_json->>'to' = 'blocked' ORDER BY event_id DESC LIMIT 1`, task).Scan(&reason))
	return reason
}

// failEach leases task to each tool in turn and fails it, rounds times, the
// backoff waited out between rounds; it returns the attempts in order.
func (w *world) failEach(t *testing.T, task string, rounds int, tools ...store.Tool) []string {
	t.Helper()
	var ids []string
	for r := 0; r < rounds; r++ {
		for _, tl := range tools {
			c := w.claim(t, "h1", store.LeaseTTL, tl)
			if c == nil || c.TaskID != task {
				t.Fatalf("round %d: %s was not leased the task: %+v", r+1, tl.Executor, c)
			}
			w.fail(t, c, "h1")
			ids = append(ids, c.AttemptID)
		}
		w.ago(t, task, 10*time.Minute)
	}
	return ids
}

// The pass blocks only when every executor that may take the task is
// withdrawn: not while one on its route has not failed it, not while one
// waits out its backoff.
func TestNoExecutorLeftNeedsEveryOneWithdrawn(t *testing.T) {
	t.Parallel()
	w := newWorldOf(t, codex, cursor, claude)
	task := w.ready(t, store.Task{})
	w.failEach(t, task, 3, codex, cursor)
	if n, err := store.BlockWithdrawn(ctx, w.rt); err != nil || n != 0 {
		t.Fatalf("blocked %d (%v) while claude has not tried the task", n, err)
	}
	c := w.claim(t, "h1", store.LeaseTTL, claude)
	if c == nil || c.TaskID != task {
		t.Fatalf("claude was not leased the task: %+v", c)
	}
	w.fail(t, c, "h1")
	if n, err := store.BlockWithdrawn(ctx, w.rt); err != nil || n != 0 {
		t.Fatalf("blocked %d (%v) while claude waits after one failure", n, err)
	}
	w.ago(t, task, 10*time.Minute)
	w.failEach(t, task, 2, claude)
	if n, err := store.BlockWithdrawn(ctx, w.rt); err != nil || n != 1 || w.state(t, "task", task) != "blocked" {
		t.Fatalf("blocked %d (%v), the task %s; want it blocked", n, err, w.state(t, "task", task))
	}
}

// An executor that may not take the task at all (here: a review of work it
// made) does not keep it ready once the others are withdrawn, and is not
// named; a pause on the task holds it as it is.
func TestNoExecutorLeftPastTheHardChecks(t *testing.T) {
	t.Parallel()
	w := newWorldOf(t, codex, cursor, claude)
	subject := w.ready(t, store.Task{})
	author := w.claim(t, "h2", store.LeaseTTL, claude)
	if author == nil || author.TaskID != subject {
		t.Fatalf("claude did not take the subject: %+v", author)
	}
	ok(t, store.Start(ctx, w.rt, fence(author, "h2"), "", ""))
	check := w.ready(t, store.Task{Role: "review", Subject: subject, Effect: "none"})
	w.failEach(t, check, 3, codex, cursor)
	if c := w.claim(t, "h1", store.LeaseTTL, claude); c != nil {
		t.Fatalf("claude was leased %+v", c)
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		return store.SetControl(ctx, tx, "owner", "c_td395", "task", check, "pause", "test")
	}))
	if n, err := store.BlockWithdrawn(ctx, w.rt); err != nil || n != 0 {
		t.Fatalf("blocked %d (%v) while a pause holds the check", n, err)
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.ClearControl(ctx, tx, "owner", "c_td395") }))
	if n, err := store.BlockWithdrawn(ctx, w.rt); err != nil || n != 1 || w.state(t, "task", check) != "blocked" {
		t.Fatalf("blocked %d (%v), the check %s; want the check blocked", n, err, w.state(t, "task", check))
	}
	if r := w.blockReason(t, check); strings.Contains(r, "exec:claude") || !strings.Contains(r, "exec:codex") || !strings.Contains(r, "exec:cursor") {
		t.Fatalf("the reason: %q", r)
	}
}

// STG's card-auto-answer rule (v3: every runtime card, 5 seconds) answers
// such a card with what it recommends, not doing the task: the task is
// cancelled and nothing is leased or asked again, where one more round
// would block it and ask again, round after round, on the mission's budget.
func TestNoExecutorLeftUnderTheCardRule(t *testing.T) {
	t.Parallel()
	w := newWorldOf(t, codex, cursor, claude)
	w.goalMission(t, "M-3")
	task := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	w.failEach(t, task, 3, codex, cursor, claude)
	signCardAutoSeconds(t, w.rt, "5", allKinds)
	if n, err := store.BlockWithdrawn(ctx, w.rt); err != nil || n != 1 {
		t.Fatalf("blocked %d (%v)", n, err)
	}
	var s cardStore
	if n, err := store.AskBlocked(ctx, w.rt, s.ask); err != nil || n != 1 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	s.aged(s.order[0], 6*time.Second)
	if n, err := store.AnswerByRule(ctx, w.rt, s.rule()); err != nil || n != 1 {
		t.Fatalf("answered %d (%v)", n, err)
	}
	if got := s.cards[s.order[0]]; got.option != "b" || got.text != "" {
		t.Fatalf("the rule answered %+v; want b, the option the card recommends", got)
	}
	if n, err := store.DecideBlocked(ctx, w.rt, s.question, s.ack); err != nil || n != 1 {
		t.Fatalf("decided %d (%v)", n, err)
	}
	if st := w.state(t, "task", task); st != "cancelled" {
		t.Fatalf("the task is %s", st)
	}
	for _, tl := range []store.Tool{codex, cursor, claude} {
		if c := w.claim(t, "h1", store.LeaseTTL, tl); c != nil {
			t.Fatalf("%s was leased %+v", tl.Executor, c)
		}
	}
	if n, err := store.BlockWithdrawn(ctx, w.rt); err != nil || n != 0 {
		t.Fatalf("blocked %d (%v)", n, err)
	}
	if n, err := store.AskBlocked(ctx, w.rt, s.ask); err != nil || n != 0 {
		t.Fatalf("asked %d (%v) after the task was dropped", n, err)
	}
}
