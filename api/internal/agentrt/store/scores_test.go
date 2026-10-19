package store_test

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// conclude runs the oldest ready task with tool on host to its end: started,
// computed, submitted, then accepted, rejected, blocked or failed (blocked as
// failed work).
func (w *world) conclude(t *testing.T, host string, tool store.Tool, to string) string {
	t.Helper()
	task := w.submit(t, host, tool)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return w.end(ctx, tx, task, to) }))
	return task
}

// submit runs the oldest ready task with tool on host to computed and
// returns it.
func (w *world) submit(t *testing.T, host string, tool store.Tool) string {
	t.Helper()
	f := w.started(t, host, tool)
	_, err := store.Computed(ctx, w.rt, f, pv(0), "exit", "done")
	ok(t, err)
	var task string
	ok(t, w.rt.QueryRow(ctx, `SELECT task_id FROM agentrt.attempt WHERE attempt_id = $1`, f.AttemptID).Scan(&task))
	return task
}

func (w *world) end(ctx context.Context, tx pgx.Tx, task, to string) error {
	last := to
	if to == "failed" {
		last = ""
	}
	for _, s := range []string{"submitted", "verifying", last} {
		if s == "" {
			return store.FailWorkForTest(ctx, tx, task)
		}
		if err := store.MoveTaskForTest(ctx, tx, "controller", task, s); err != nil {
			return err
		}
	}
	return nil
}

// computation is the latest capability_scored body.
func (w *world) computation(t *testing.T) map[string]any {
	t.Helper()
	var body map[string]any
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json FROM agentrt.event WHERE event_stream_id = $1 ORDER BY seq DESC LIMIT 1`,
		store.ScoresStream).Scan(&body))
	return body
}

// sample is the sample of the latest computation for an executor and provider.
func (w *world) sample(t *testing.T, executor, provider string) string {
	t.Helper()
	for _, s := range w.computation(t)["samples"].([]any) {
		m := s.(map[string]any)
		if m["executor"] == executor && m["provider"] == provider {
			return fmt.Sprintf("%v of %v", m["first_pass"], m["attempts"])
		}
	}
	return "none"
}

func TestWilson(t *testing.T) {
	for _, c := range []struct {
		k, n int
		want float64
	}{{0, 0, 0}, {0, 5, 0}, {5, 5, 0.566}, {1, 3, 0.0615}, {90, 100, 0.8256}} {
		if got := store.Wilson(c.k, c.n); math.Abs(got-c.want) > 0.001 {
			t.Errorf("Wilson(%d, %d) = %.4f, want %.4f", c.k, c.n, got, c.want)
		}
	}
}

// W-31 2.4 slice ②: a task that ends counts for the executor of its last
// attempt; first pass only in round 1 at attempt 1; rework when it took more
// rounds; a computation is made only when a task ended since the last one.
func TestInJobScores(t *testing.T) {
	w := newKernelWorld(t)
	w.ready(t, store.Task{})
	w.conclude(t, "h1", codex, "accepted") // first pass
	w.ready(t, store.Task{})
	rework := w.conclude(t, "h2", codex, "accepted")
	w.legacy(t, `UPDATE agentrt.task SET round_no = 2 WHERE task_id = '`+rework+`'`)
	w.ready(t, store.Task{})
	w.conclude(t, "h3", codex, "rejected")
	n, err := store.ComputeScores(ctx, w.rt)
	ok(t, err)
	if n != 1 {
		t.Fatalf("%d rows; want one group", n)
	}
	var attempts, firstPass, accepted, reworks int
	var score float64
	var job, class, source string
	ok(t, w.rt.QueryRow(ctx, `SELECT attempts, first_pass, accepted, rework, score::float8, job_id, task_class, source
	  FROM agentrt.capability_score WHERE executor_actor_id = 'exec:codex'`).Scan(&attempts, &firstPass, &accepted, &reworks, &score, &job, &class, &source))
	if attempts != 3 || firstPass != 1 || accepted != 2 || reworks != 1 || job != "implementer" || class != "go" || source != "in_job" ||
		math.Abs(score-store.Wilson(1, 3)) > 1e-9 {
		t.Fatalf("score row: %d attempts, %d first pass, %d accepted, %d rework, %v, %s %s %s", attempts, firstPass, accepted, reworks, score, job, class, source)
	}
	if n, err = store.ComputeScores(ctx, w.rt); err != nil || n != 0 {
		t.Fatalf("with nothing new: %d rows, %v", n, err)
	}
	w.ready(t, store.Task{})
	w.conclude(t, "h4", claude, "accepted")
	if n, err = store.ComputeScores(ctx, w.rt); err != nil || n != 2 {
		t.Fatalf("after a new conclusion: %d rows (want codex and claude), %v", n, err)
	}
}

// Review 286 B2: work blocked as failed counts as a failure; a task blocked
// for another reason counts for nobody.
func TestFailedWorkCounts(t *testing.T) {
	w := newKernelWorld(t)
	w.ready(t, store.Task{})
	w.conclude(t, "h1", codex, "accepted")
	w.ready(t, store.Task{})
	w.conclude(t, "h2", codex, "failed")
	w.ready(t, store.Task{})
	w.conclude(t, "h3", codex, "blocked")
	_, err := store.ComputeScores(ctx, w.rt)
	ok(t, err)
	if got := w.sample(t, "exec:codex", "openai"); got != "1 of 2" {
		t.Fatalf("codex = %s; want 1 of 2 (the failed one counted, the merely blocked one not)", got)
	}
}

// Review 286 B1: one executor and model under two providers in one
// computation: both samples kept, nothing rolled back.
func TestTwoProvidersOfOneModel(t *testing.T) {
	w := newKernelWorld(t)
	other := store.Tool{Executor: "exec:claude", Provider: "cursor-mix", Model: "opus"}
	w.ready(t, store.Task{})
	w.conclude(t, "h1", claude, "accepted")
	w.ready(t, store.Task{})
	w.conclude(t, "h2", other, "failed")
	_, err := store.ComputeScores(ctx, w.rt)
	ok(t, err)
	if a, b := w.sample(t, "exec:claude", "anthropic"), w.sample(t, "exec:claude", "cursor-mix"); a != "1 of 1" || b != "0 of 1" {
		t.Fatalf("claude/anthropic %s, claude/cursor-mix %s", a, b)
	}
}

// Review 286 B3: an outcome whose transaction took its event id first and
// commits after a later one's computation is counted by the next.
func TestALateOutcomeIsCounted(t *testing.T) {
	w := newKernelWorld(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-2", "active", "test") }))
	w.ready(t, store.Task{})
	early := w.submit(t, "h1", codex)
	pool := w.db.Pool(t, w.db.Runtime)
	tx, err := pool.Begin(ctx)
	ok(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	ok(t, w.end(ctx, tx, early, "accepted")) // its events now hold the lower ids, uncommitted
	w.ready(t, store.Task{MissionID: "M-2", OffPlanReason: "test"})
	w.conclude(t, "h2", codex, "failed")
	_, err = store.ComputeScores(ctx, w.rt)
	ok(t, err)
	if got := w.sample(t, "exec:codex", "openai"); got != "0 of 1" {
		t.Fatalf("before the early one commits: %s", got)
	}
	ok(t, tx.Commit(ctx))
	n, err := store.ComputeScores(ctx, w.rt)
	ok(t, err)
	if got := w.sample(t, "exec:codex", "openai"); n == 0 || got != "1 of 2" {
		t.Fatalf("after it commits: %d rows, codex %s", n, got)
	}
	if n, err = store.ComputeScores(ctx, w.rt); err != nil || n != 0 {
		t.Fatalf("once it is counted: %d rows (%v)", n, err)
	}
}

// Review 286 B4: a computation whose earliest sample leaves the window is made
// again, with nothing new, and drops it.
func TestASampleLeavesTheWindow(t *testing.T) {
	w := newKernelWorld(t)
	w.ready(t, store.Task{})
	task := w.conclude(t, "h1", codex, "failed")
	edge := time.Now().Add(-store.ScoreWindow + 1500*time.Millisecond).UTC().Format(time.RFC3339Nano)
	w.legacy(t, `UPDATE agentrt.task SET updated_at = '`+edge+`' WHERE task_id = '`+task+`'`)
	_, err := store.ComputeScores(ctx, w.rt)
	ok(t, err)
	if got := w.sample(t, "exec:codex", "openai"); got != "0 of 1" {
		t.Fatalf("inside the window: %s", got)
	}
	time.Sleep(2 * time.Second)
	_, err = store.ComputeScores(ctx, w.rt)
	ok(t, err)
	if got := w.sample(t, "exec:codex", "openai"); got != "none" {
		t.Fatalf("after it left the window: %s", got)
	}
	if _, has := w.computation(t)["expires_at"]; has {
		t.Fatalf("a computation with no samples expires: %v", w.computation(t))
	}
}
