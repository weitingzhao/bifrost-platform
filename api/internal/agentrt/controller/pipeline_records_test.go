//bifrost:owner verification

package controller_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
)

// noDB fails every transaction: a pass that reads the database fails with it.
type noDB struct{}

var errNoDB = errors.New("the database was read")

func (noDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) { return nil, errNoDB }

func completedRun(name string) seenfacts.PipelineRun {
	at := time.Date(2026, 11, 12, 21, 41, 54, 0, time.UTC)
	return seenfacts.PipelineRun{Run: name, UID: "uid-" + name, Pipeline: "bifrost-deliver-platform-prod", Kind: "delivery", Result: "Succeeded",
		Repo: "bifrost-platform", Revision: strings.Repeat("a", 40), CreatedAt: at.Add(-5 * time.Minute), StartedAt: at.Add(-5 * time.Minute), CompletedAt: at}
}

func pipelineSeenCount(t *testing.T, db eventlog.DB, run string) int {
	t.Helper()
	var n int
	must(t, eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'pipeline_seen' AND body_json->>'run' = $1`, run).Scan(&n)
	}))
	return n
}

// A pass over an unchanged list reads the list and not the database; a run
// that completed since is recorded and told once; a run the cluster no longer
// retains leaves the memo.
func TestPipelineRecordsRemembersWhatItRecorded(t *testing.T) {
	t.Parallel()
	pool := setup(t)
	runs := []seenfacts.PipelineRun{completedRun("deliver-1")}
	told := 0
	c := controller.Controller{DB: pool, PipelineRecorded: func() { told++ }, Sources: controller.Sources{
		PipelineRuns: func(context.Context) ([]seenfacts.PipelineRun, error) { return runs, nil }}}
	pass := func(db eventlog.DB, want, wantTold int) {
		t.Helper()
		c.DB = db
		n, err := c.PipelineRecords(ctx)
		if err != nil || n != want || told != wantTold {
			t.Fatalf("pass: recorded %d (want %d), told %d (want %d), err %v", n, want, told, wantTold, err)
		}
	}
	pass(pool, 1, 1)
	pass(noDB{}, 0, 1) // unchanged list: the database is not read
	pass(noDB{}, 0, 1)

	runs = append(runs, completedRun("deliver-2"))
	c.DB = noDB{}
	if n, err := c.PipelineRecords(ctx); n != 0 || !errors.Is(err, errNoDB) || told != 1 {
		t.Fatalf("a new run with the database away: %d %v told %d", n, err, told)
	}
	pass(pool, 1, 2) // not remembered while unrecorded: recorded once the database answers
	pass(noDB{}, 0, 2)
	if got := pipelineSeenCount(t, pool, "deliver-1") + pipelineSeenCount(t, pool, "deliver-2"); got != 2 {
		t.Fatalf("events: %d", got)
	}

	// The run leaves the list, then comes back: it was forgotten, so it is
	// compared with the database again, and that finds it recorded.
	runs = runs[1:]
	pass(noDB{}, 0, 2)
	runs = append(runs, completedRun("deliver-1"))
	c.DB = noDB{}
	if _, err := c.PipelineRecords(ctx); !errors.Is(err, errNoDB) {
		t.Fatalf("a run back in the list was still remembered: %v", err)
	}
	pass(pool, 0, 2)
	if got := pipelineSeenCount(t, pool, "deliver-1"); got != 1 {
		t.Fatalf("deliver-1 recorded %d times", got)
	}
}

// Every PipelineFullEvery a pass compares every listed run with the database
// whatever it remembers (so a record the database lost would be written
// again), and a run the database still has is not recorded twice.
func TestPipelineRecordsFullPassComparesEveryRunAgain(t *testing.T) {
	t.Parallel()
	pool := setup(t)
	runs := []seenfacts.PipelineRun{completedRun("deliver-1")}
	c := controller.Controller{DB: pool, PipelineFullEvery: time.Hour, Sources: controller.Sources{
		PipelineRuns: func(context.Context) ([]seenfacts.PipelineRun, error) { return runs, nil }}}
	if n, err := c.PipelineRecords(ctx); n != 1 || err != nil {
		t.Fatalf("first pass: %d %v", n, err)
	}
	c.DB = noDB{}
	if n, err := c.PipelineRecords(ctx); n != 0 || err != nil {
		t.Fatalf("a pass within the interval read the database: %d %v", n, err)
	}
	c.PipelineFullEvery = time.Nanosecond
	if _, err := c.PipelineRecords(ctx); !errors.Is(err, errNoDB) {
		t.Fatalf("a full pass did not read the database: %v", err)
	}
	c.DB = pool
	if n, err := c.PipelineRecords(ctx); n != 0 || err != nil || pipelineSeenCount(t, pool, "deliver-1") != 1 {
		t.Fatalf("a full pass over a recorded run: %d %v", n, err)
	}
}

// A run the vocabulary refuses is reported at every pass and does not keep the
// others from being recorded and remembered.
func TestPipelineRecordsRefusedRunDoesNotHoldTheOthers(t *testing.T) {
	t.Parallel()
	pool := setup(t)
	bad := completedRun("deliver-bad")
	bad.Kind = "nightly"
	runs := []seenfacts.PipelineRun{bad, completedRun("deliver-ok")}
	c := controller.Controller{DB: pool, Sources: controller.Sources{
		PipelineRuns: func(context.Context) ([]seenfacts.PipelineRun, error) { return runs, nil }}}
	n, err := c.PipelineRecords(ctx)
	if n != 1 || !errors.Is(err, eventlog.ErrInvalid) || !strings.Contains(err.Error(), "deliver-bad") {
		t.Fatalf("first pass: %d %v", n, err)
	}
	c.DB = noDB{}
	n, err = c.PipelineRecords(ctx)
	if n != 0 || !errors.Is(err, eventlog.ErrInvalid) || errors.Is(err, errNoDB) {
		t.Fatalf("second pass read the database or dropped the refusal: %d %v", n, err)
	}
	if pipelineSeenCount(t, pool, "deliver-bad") != 0 || pipelineSeenCount(t, pool, "deliver-ok") != 1 {
		t.Fatal("recorded the refused run or not the other")
	}
}

// The loop's reconciliation records a run that completed after start, one the
// bus did not name (since 2026-11-18, L02, the loop passes once per
// PipelineReconcileEvery).
func TestPipelineRecordsLoopRecordsACompletedRunAtItsReconciliation(t *testing.T) {
	t.Parallel()
	pool := setup(t)
	var mu sync.Mutex
	var runs []seenfacts.PipelineRun
	var told atomic.Int32
	var ahead atomic.Int64
	c := &controller.Controller{DB: pool, Now: func() time.Time { return time.Now().Add(time.Duration(ahead.Load())) },
		LeaseEvery: time.Hour, FactsEvery: time.Hour, PipelineEvery: 20 * time.Millisecond,
		PipelineRecorded: func() { told.Add(1) },
		Sources: controller.Sources{PipelineRuns: func(context.Context) ([]seenfacts.PipelineRun, error) {
			mu.Lock()
			defer mu.Unlock()
			return append([]seenfacts.PipelineRun(nil), runs...), nil
		}}}
	run, cancel := context.WithCancel(ctx)
	wait := c.Start(run)
	defer wait()
	defer cancel()
	time.Sleep(60 * time.Millisecond) // the startup backfill and a pass or two see no run
	mu.Lock()
	runs = []seenfacts.PipelineRun{completedRun("deliver-late")}
	mu.Unlock()
	ahead.Store(int64(controller.PipelineReconcileEvery))
	for deadline := time.Now().Add(10 * time.Second); pipelineSeenCount(t, pool, "deliver-late") != 1 || told.Load() != 1; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the loop did not record the run: events %d, told %d", pipelineSeenCount(t, pool, "deliver-late"), told.Load())
		}
	}
	time.Sleep(100 * time.Millisecond) // further passes record and tell nothing more
	if pipelineSeenCount(t, pool, "deliver-late") != 1 || told.Load() != 1 {
		t.Fatalf("recorded or told again: events %d, told %d", pipelineSeenCount(t, pool, "deliver-late"), told.Load())
	}
}

// A done step with no standing condition cannot move again, so a pass leaves
// its row alone: a pass that locked it would wait here for the holder.
//
// Origin: 2026-11-19, PROD's agentrt-facts-rules locked 131 such rows each pass.
func TestRulesLeavesASettledStepUnlocked(t *testing.T) {
	pool := setup(t)
	setRule(t, pool, "R2:1-1", `{"owner_signoff":"settled"}`)
	must(t, eventlog.InTx(ctx, pool, func(tx pgx.Tx) error {
		_, err := planstore.SetStepState(ctx, tx, "controller", "R2", "R2:1-1", "done", "test")
		return err
	}))
	held, err := pool.Begin(ctx)
	must(t, err)
	defer func() { _ = held.Rollback(ctx) }()
	_, err = held.Exec(ctx, `SELECT 1 FROM agentrt.plan_node WHERE plan_node_id = 'R2:1-1' FOR UPDATE`)
	must(t, err)
	short, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	c := &controller.Controller{DB: pool}
	moved, err := c.Rules(short)
	must(t, err)
	if moved != 0 {
		t.Fatalf("moved %d steps", moved)
	}
}

// A deployment read that needs the runtime's pool of its own still works when
// the pool has a single connection: the read happens before the record's
// transaction takes it, so the deployment is recorded at once instead of
// after the read timeout, with nothing recorded.
//
// Origin: 2026-11-19, STG's facts step took 10.004-10.009 s each pass: its
// pool of two had one connection on the bus listener and the record's
// transaction held the other while the deployment read asked for a third.
func TestDeployReadDoesNotWaitForTheRecordsConnection(t *testing.T) {
	pool := setup(t)
	setRule(t, pool, "R2:1-1", `{"gates_passed":[{"test":"T19","spec_sha256":"`+strings.Repeat("ab", 32)+`"}],"env":"stg"}`)
	cfg, err := pgxpool.ParseConfig(pool.Config().ConnString())
	must(t, err)
	cfg.MaxConns = 1
	one, err := pgxpool.NewWithConfig(ctx, cfg)
	must(t, err)
	defer one.Close()
	c := &controller.Controller{DB: one, ReadTimeout: 2 * time.Second, Sources: controller.Sources{
		Deployed: func(ctx context.Context, _ string) (string, map[string]any, bool) {
			var n int
			if err := one.QueryRow(ctx, `SELECT 1`).Scan(&n); err != nil {
				return "", nil, false
			}
			return strings.Repeat("c", 40), map[string]any{"delivery_run": "run-1"}, true
		},
	}}
	start := time.Now()
	n, err := c.Facts(ctx)
	must(t, err)
	if n != 1 || time.Since(start) > time.Second {
		t.Fatalf("recorded %d after %v; want the deployment at once", n, time.Since(start))
	}
}
