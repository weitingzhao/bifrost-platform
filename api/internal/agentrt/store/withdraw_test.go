package store_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// proposedBy proposes the batch on M-1 as the actor and gives each ref's id.
func (w *world) proposedBy(t *testing.T, actor, key string, batch ...store.TaskProposal) map[string]string {
	t.Helper()
	out, err := store.ProposeTasks(ctx, w.rt, wr(actor), key, "M-1", batch)
	ok(t, err)
	return taskIDs(out)
}

func (w *world) markReady(t *testing.T, ids ...string) {
	t.Helper()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		for _, id := range ids {
			if err := store.MarkReady(ctx, tx, "controller", id); err != nil {
				return err
			}
		}
		return nil
	}))
}

// withdrawal is the actor and reason of the task's latest move to cancelled.
func (w *world) withdrawal(t *testing.T, task string) (actor, reason string) {
	t.Helper()
	ok(t, w.rt.QueryRow(ctx, `SELECT actor_id, body_json->>'reason' FROM agentrt.event WHERE task_id = $1
	  AND event_type = 'task_state_changed' AND body_json->>'to' = 'cancelled' ORDER BY event_id DESC LIMIT 1`, task).Scan(&actor, &reason))
	return actor, reason
}

// The thread that proposed a ready task withdraws it: cancelled, the move
// the thread's, its reason saying who and why on one line.
func TestWithdrawReadyTaskByItsProposer(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	a := w.proposedBy(t, "thread:s1", "p1", proposal("a", w.impl))["a"]
	w.markReady(t, a)
	out, err := store.WithdrawTasks(ctx, w.rt, wr("thread:s1"), "w1", []string{a}, "the spec was wrong:\n  it names the wrong repo")
	ok(t, err)
	same(t, out, map[string]any{"withdrawn": []any{a}, "checks_cancelled": []any{}, "by": "thread:s1"})
	if s := w.state(t, "task", a); s != "cancelled" {
		t.Fatalf("task is %s", s)
	}
	actor, reason := w.withdrawal(t, a)
	if actor != "thread:s1" || reason != "withdrawn by thread:s1: the spec was wrong: it names the wrong repo" {
		t.Fatalf("move by %s: %q", actor, reason)
	}
	long := strings.Repeat("é", 600)
	b := w.proposedBy(t, "thread:s1", "p2", proposal("b", w.impl))["b"]
	_, err = store.WithdrawTasks(ctx, w.rt, wr("thread:s1"), "w2", []string{b}, long)
	ok(t, err)
	if _, reason = w.withdrawal(t, b); len([]rune(reason)) != 500 || !strings.HasPrefix(reason, "withdrawn by thread:s1: éé") {
		t.Fatalf("a long reason is %d runes: %.40q", len([]rune(reason)), reason)
	}
	w.chainHolds(t, "mission:M-1")
}

// The mission's proposer withdraws a task another actor proposed in it.
func TestWithdrawByTheMissionsProposer(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	m, err := store.ProposeMission(ctx, w.rt, wr("thread:lead"), "m1", mission, nil)
	ok(t, err)
	id := m["mission_id"].(string)
	out, err := store.ProposeTasks(ctx, w.rt, wr("seat:chief_of_staff"), "p1", id, []store.TaskProposal{offPlan(proposal("a", w.impl))})
	ok(t, err)
	a := taskIDs(out)["a"]
	_, err = store.WithdrawTasks(ctx, w.rt, wr("thread:lead"), "w1", []string{a}, "not needed")
	ok(t, err)
	if s := w.state(t, "task", a); s != "cancelled" {
		t.Fatalf("task is %s", s)
	}
	if actor, _ := w.withdrawal(t, a); actor != "thread:lead" {
		t.Fatalf("move by %s", actor)
	}
}

// Another thread, the Owner and the controller are refused, and nothing
// moves; so is a request that breaks a rule of its own.
func TestWithdrawRefusesWhoDidNotPropose(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	a := w.proposedBy(t, "thread:s1", "p1", proposal("a", w.impl))["a"]
	w.markReady(t, a)
	_, err := store.ProposeMission(ctx, w.rt, wr("thread:other"), "m1", mission, nil)
	ok(t, err)
	before := w.footprint(t)
	for _, actor := range []string{"thread:other", "owner", "controller"} {
		_, err := store.WithdrawTasks(ctx, w.rt, wr(actor), "w1", []string{a}, "mine now")
		is(t, err, store.ErrNotYours)
	}
	for name, c := range map[string]struct {
		key, reason string
		ids         []string
	}{
		"no key":    {"", "why", []string{a}},
		"no reason": {"w2", " ", []string{a}},
		"no ids":    {"w3", "why", nil},
		"twice":     {"w4", "why", []string{a, a}},
		"no task":   {"w5", "why", []string{a, noSuch}},
		"too many":  {"w6", "why", make([]string, store.MaxBatch+1)},
	} {
		if _, err := store.WithdrawTasks(ctx, w.rt, wr("thread:s1"), c.key, c.ids, c.reason); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if after := w.footprint(t); after != before {
		t.Fatalf("refusals left something: %s -> %s", before, after)
	}
	if s := w.state(t, "task", a); s != "ready" {
		t.Fatalf("task is %s", s)
	}
}

// A task with a leased attempt, and one whose attempt ended, are not
// withdrawn: work that started is not.
func TestWithdrawRefusesStartedWork(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	leased := w.proposedBy(t, "thread:s1", "p1", proposal("a", w.impl))["a"]
	w.markReady(t, leased)
	w.claimed(t, leased, "h1", codex, false)
	ended := w.proposedBy(t, "thread:s1", "p2", proposal("b", w.impl))["b"]
	w.markReady(t, ended)
	c := w.claimed(t, ended, "h2", codex, false)
	_, err := store.Ended(ctx, w.rt, c.AttemptID, "h2", "daemon_restart", false)
	ok(t, err)
	if s := w.state(t, "task", ended); s != "ready" {
		t.Fatalf("the task whose attempt ended is %s", s)
	}
	before := w.footprint(t)
	for _, id := range []string{leased, ended} {
		_, err := store.WithdrawTasks(ctx, w.rt, wr("thread:s1"), "w-"+id, []string{id}, "wrong spec")
		is(t, err, store.ErrState)
		if !strings.Contains(err.Error(), id) {
			t.Fatalf("the refusal does not name %s: %v", id, err)
		}
	}
	if after := w.footprint(t); after != before {
		t.Fatalf("refusals left something: %s -> %s", before, after)
	}
}

// A blocked task with no attempt is withdrawn.
func TestWithdrawBlockedTask(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	a := w.proposedBy(t, "thread:s1", "p1", proposal("a", w.impl))["a"]
	w.markReady(t, a)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveTaskForTest(ctx, tx, "controller", a, "blocked") }))
	_, err := store.WithdrawTasks(ctx, w.rt, wr("thread:s1"), "w1", []string{a}, "abandoned")
	ok(t, err)
	if s := w.state(t, "task", a); s != "cancelled" {
		t.Fatalf("task is %s", s)
	}
}

// A task an unfinished task outside the list depends on is refused, naming
// the dependent; with the dependent in the list both are withdrawn.
func TestWithdrawDependents(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	id := w.proposedBy(t, "thread:s1", "p1", proposal("a", w.impl), proposal("b", w.impl, "a"))
	before := w.footprint(t)
	_, err := store.WithdrawTasks(ctx, w.rt, wr("thread:s1"), "w1", []string{id["a"]}, "wrong spec")
	is(t, err, store.ErrState)
	if !strings.Contains(err.Error(), id["b"]) {
		t.Fatalf("the refusal does not name the dependent %s: %v", id["b"], err)
	}
	if after := w.footprint(t); after != before {
		t.Fatalf("the refusal left something: %s -> %s", before, after)
	}
	out, err := store.WithdrawTasks(ctx, w.rt, wr("thread:s1"), "w2", []string{id["b"], id["a"]}, "wrong spec")
	ok(t, err)
	if got := out["withdrawn"].([]any); len(got) != 2 {
		t.Fatalf("answer %v", out)
	}
	for _, ref := range []string{"a", "b"} {
		if s := w.state(t, "task", id[ref]); s != "cancelled" {
			t.Fatalf("%s is %s", ref, s)
		}
	}
}

// An unfinished check of a withdrawn task is cancelled with it.
func TestWithdrawCancelsChecks(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	a := w.proposedBy(t, "thread:s1", "p1", proposal("a", w.impl))["a"]
	check, err := w.task(t, "M-1", s1, a, "verify")
	ok(t, err)
	out, err := store.WithdrawTasks(ctx, w.rt, wr("thread:s1"), "w1", []string{a}, "wrong spec")
	ok(t, err)
	same(t, out, map[string]any{"withdrawn": []any{a}, "checks_cancelled": []any{check}, "by": "thread:s1"})
	if s := w.state(t, "task", check); s != "cancelled" {
		t.Fatalf("check is %s", s)
	}
	if actor, _ := w.withdrawal(t, check); actor != "thread:s1" {
		t.Fatalf("check's move by %s", actor)
	}
}

// A retry with the same key gets the first answer and writes nothing; the
// same key with another request is a conflict.
func TestWithdrawRetry(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	id := w.proposedBy(t, "thread:s1", "p1", proposal("a", w.impl), proposal("b", w.impl))
	first, err := store.WithdrawTasks(ctx, w.rt, wr("thread:s1"), "w1", []string{id["b"], id["a"]}, "wrong spec")
	ok(t, err)
	before := w.footprint(t)
	again, err := store.WithdrawTasks(ctx, w.rt, wr("thread:s1"), "w1", []string{id["a"], id["b"]}, "wrong spec")
	ok(t, err)
	same(t, again, first)
	_, err = store.WithdrawTasks(ctx, w.rt, wr("thread:s1"), "w1", []string{id["a"]}, "wrong spec")
	is(t, err, store.ErrConflict)
	if after := w.footprint(t); after != before {
		t.Fatalf("the retry wrote: %s -> %s", before, after)
	}
}
