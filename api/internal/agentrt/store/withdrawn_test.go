package store_test

import (
	"fmt"
	"slices"
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

// TD-395 (PROD M-22, 2026-10-20): three executors each failing a task three
// times in a row left it at ready, no host offered it, no card. Now the
// controller's pass blocks it, naming each executor and the attempts it
// failed, and its card recommends not doing it (R6). One more round offers
// it once to each executor again; one more failure each blocks it again,
// with a new card that recommends the same. Not doing it cancels it, and
// its mission finishes.
func TestNoExecutorLeftBlocksWithACard(t *testing.T) {
	t.Parallel()
	w := newWorldOf(t, codex, cursor, claude)
	w.goalMission(t, "M-3")
	task := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	w.failEach(t, task, 3, codex, cursor, claude)
	for _, tl := range []store.Tool{codex, cursor, claude} {
		if c := w.claim(t, "h1", store.LeaseTTL, tl); c != nil {
			t.Fatalf("%s was leased the task after three failures in a row: %+v", tl.Executor, c)
		}
	}
	if s := w.state(t, "task", task); s != "ready" {
		t.Fatalf("the task is %s before the pass", s)
	}
	if n, err := store.BlockWithdrawn(ctx, w.rt); err != nil || n != 1 {
		t.Fatalf("blocked %d (%v); want the task", n, err)
	}
	if s := w.state(t, "task", task); s != "blocked" {
		t.Fatalf("the task is %s after the pass; want blocked", s)
	}
	reason := w.blockReason(t, task)
	for _, want := range []string{"no executor on its route may take it any more: ",
		"exec:codex failed attempts 1, 4, 7 in a row (the last: exit)", "exec:cursor failed attempts 2, 5, 8 in a row (the last: exit)",
		"exec:claude failed attempts 3, 6, 9 in a row (the last: exit)", "after 3 failures in a row of its own doing"} {
		if !strings.Contains(reason, want) {
			t.Fatalf("the block's reason %q lacks %q", reason, want)
		}
	}
	if n, err := store.BlockWithdrawn(ctx, w.rt); err != nil || n != 0 {
		t.Fatalf("a second pass blocked %d (%v)", n, err)
	}

	a := &asker{}
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	c := a.cards[0]
	why := "every executor on its route failed it in a row (claude, codex, cursor); another round is worth it only once what failed is fixed"
	if c.TaskID != task || c.Recommended != 2 || c.RecommendedWhy != why ||
		!slices.Equal(c.Options, []string{store.BlockedRetry, store.BlockedDrop, "Close mission M-3"}) {
		t.Fatalf("card %+v", c)
	}
	for _, want := range []string{"Recommended: Do not do this task — " + why, "Why: no executor on its route may take it any more: exec:claude",
		store.BlockedRetry + ": it goes back to ready, offered once more to each executor withdrawn from it; one more failure withdraws it again"} {
		if !strings.Contains(c.Body, want) {
			t.Fatalf("the card lacks %q:\n%s", want, c.Body)
		}
	}

	// One more round: each executor is offered the task once more, at once.
	a.answer(a.lastID(), store.QuestionAnswered, store.CardApplyKey)
	if n, err := store.DecideBlocked(ctx, w.rt, a.question, a.ack); err != nil || n != 1 {
		t.Fatalf("decided %d (%v)", n, err)
	}
	if s := w.state(t, "task", task); s != "ready" || !strings.Contains(w.lastReason(t, task), "offering it once more to each executor withdrawn from it") {
		t.Fatalf("the task is %s, %q", s, w.lastReason(t, task))
	}
	again := w.claim(t, "h1", store.LeaseTTL, codex)
	if again == nil || again.TaskID != task {
		t.Fatalf("codex was not offered the task again at once: %+v", again)
	}
	w.fail(t, again, "h1")
	if c := w.claim(t, "h1", store.LeaseTTL, codex); c != nil {
		t.Fatalf("codex was leased the task again after its failure past the round: %+v", c)
	}
	w.ago(t, task, time.Hour)
	if c := w.claim(t, "h1", store.LeaseTTL, codex); c != nil {
		t.Fatalf("codex was leased the task an hour after its failure past the round: %+v", c)
	}
	if n, err := store.BlockWithdrawn(ctx, w.rt); err != nil || n != 0 {
		t.Fatalf("blocked %d (%v) while cursor and claude are still offered it", n, err)
	}
	w.failEach(t, task, 1, cursor, claude)
	if n, err := store.BlockWithdrawn(ctx, w.rt); err != nil || n != 1 {
		t.Fatalf("blocked %d (%v) after every executor failed once more", n, err)
	}
	if !strings.Contains(w.blockReason(t, task), "exec:codex failed attempts 1, 4, 7, 10 in a row") {
		t.Fatalf("the second block: %q", w.blockReason(t, task))
	}
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 || len(a.cards) != 2 {
		t.Fatalf("asked %d (%v), %d cards", n, err, len(a.cards))
	}
	if c := a.cards[1]; c.Recommended != 2 || c.RecommendedWhy != why {
		t.Fatalf("the second card recommends %d %q", c.Recommended, c.RecommendedWhy)
	}
	a.answer(a.lastID(), store.QuestionAnswered, store.CardRefuseKey)
	if n, err := store.DecideBlocked(ctx, w.rt, a.question, a.ack); err != nil || n != 1 {
		t.Fatalf("decided %d (%v)", n, err)
	}
	if s := w.state(t, "task", task); s != "cancelled" {
		t.Fatalf("the task is %s; want cancelled", s)
	}
	if _, err := store.CloseMissions(ctx, w.rt); err != nil {
		t.Fatal(err)
	}
	if s := w.state(t, "mission", "M-3"); s == "active" {
		t.Fatalf("M-3 is %s with its only task not done", s)
	}
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
