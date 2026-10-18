package store_test

import (
	"math"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// conclude runs the oldest ready task with tool on host to its end: started,
// computed, submitted, then accepted or rejected.
func (w *world) conclude(t *testing.T, host string, tool store.Tool, to string) string {
	t.Helper()
	f := w.started(t, host, tool)
	_, err := store.Computed(ctx, w.rt, f, pv(0), "exit", "done")
	ok(t, err)
	var task string
	ok(t, w.rt.QueryRow(ctx, `SELECT task_id FROM agentrt.attempt WHERE attempt_id = $1`, f.AttemptID).Scan(&task))
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		for _, s := range []string{"submitted", "verifying", to} {
			if err := store.MoveTaskForTest(ctx, tx, "controller", task, s); err != nil {
				return err
			}
		}
		return nil
	}))
	return task
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
