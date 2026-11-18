package server

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	bushook "github.com/weitingzhao/bifrost-platform/api/internal/bus/hook"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/relay"
)

// 2026-11-16: bus step 1 (RULES.md §7): the relay's and the gate's reading
// of the ledger is eventlog.EventsAfter's, through the wiring: an event gate-git
// appends wakes a listener at its commit (eventlog.Append's pg_notify) and is
// read after the horizon taken before it, as an envelope of its type.
func TestBusReadsTheLedgerThroughEventsAfter(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := agentrttest.Migrated(t)
	if _, err := actor.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pool := db.Pool(t, db.Runtime)
	ledger := &relay.PGLedger{DB: func() relay.TxBeginner { return pool }, EventsAfter: busEventsAfter}
	from, err := ledger.Horizon(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wake := relay.Listen(ctx, pool, t.Logf)
	select {
	case <-wake: // the LISTEN is in place
	case <-time.After(10 * time.Second):
		t.Fatal("the listener did not start")
	}
	commit := strings.Repeat("c", 40)
	if ok, aerr := bushook.Advance(ctx, pool, "bifrost-platform", commit, "", bushook.ChangePushed); aerr != nil || !ok {
		t.Fatalf("appended %v %v", ok, aerr)
	}
	select {
	case <-wake:
	case <-time.After(5 * time.Second):
		t.Fatal("no wake at the commit")
	}
	// An event shows once every older transaction of the cluster has ended
	// (eventlog.EventsAfter's horizon): other tests' databases share the server.
	var rows []relay.Row
	var next relay.Cursor
	for deadline := time.Now().Add(10 * time.Second); len(rows) == 0 && err == nil && time.Now().Before(deadline); {
		if rows, next, err = ledger.After(ctx, from, 100, []string{bushook.Type}); len(rows) == 0 {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if err != nil || len(rows) != 1 || !rows[0].Cursor().Less(next) {
		t.Fatalf("rows %+v next %s %v", rows, next, err)
	}
	all, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	e, _, err := relay.Build(rows[0], all[relay.Tenant].Channels[bushook.Type], relay.Tenant, "dev")
	if err != nil || e.Subject != "bifrost-platform@"+commit || e.Actor != bushook.Actor {
		t.Fatalf("envelope %+v %v", e, err)
	}
	if row, found, err := ledger.Event(ctx, rows[0].EventID); err != nil || !found || row.Cursor() != rows[0].Cursor() {
		t.Fatalf("by id: %+v %v %v", row, found, err)
	}
	if rows, _, err := ledger.After(ctx, next, 100, []string{bushook.Type}); err != nil || len(rows) != 0 {
		t.Fatalf("read again after next: %d %v", len(rows), err)
	}
}

// 2026-11-16: bus step 1: the bus's kick runs head-release's own pass, one
// at a time with the controller's loop; with the switch off nothing is wrapped.
func TestHeadReleaseKickRunsTheSamePass(t *testing.T) {
	t.Parallel()
	var b busWiring
	if b.serialHeadRelease(nil) != nil {
		t.Fatal("an off switch became a pass")
	}
	var runs, inside atomic.Int32
	pass := b.serialHeadRelease(func(context.Context) error {
		if inside.Add(1) > 1 {
			t.Error("two passes at once")
		}
		time.Sleep(50 * time.Millisecond)
		inside.Add(-1)
		runs.Add(1)
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.runKicks(ctx)
	b.kickHeadRelease()
	if err := pass(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for runs.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if runs.Load() != 2 {
		t.Fatalf("%d passes", runs.Load())
	}
}

// 2026-11-16: with the bus waking head-release (51 s → 11 s), its 30-second
// loop became a 5-minute reconciliation; without the bus it keeps every tick.
func TestHeadReleaseLoopReconcilesWhileTheBusWakesIt(t *testing.T) {
	now := time.Date(2026, 11, 16, 20, 0, 0, 0, time.UTC)
	runs := 0
	var live bool
	p := &serialPass{pass: func(context.Context) error { runs++; return nil },
		live: func() bool { return live }, now: func() time.Time { return now }}
	ctx := context.Background()
	_ = p.tick(ctx)
	now = now.Add(30 * time.Second)
	_ = p.tick(ctx)
	if runs != 2 {
		t.Fatalf("bus off: %d passes in two ticks, want 2", runs)
	}
	live = true
	now = now.Add(30 * time.Second)
	_ = p.tick(ctx)
	if runs != 2 {
		t.Fatalf("bus on: a tick 30 s after a pass ran one (%d)", runs)
	}
	now = now.Add(headReleaseReconcileEvery)
	_ = p.tick(ctx)
	if runs != 3 {
		t.Fatalf("bus on: no reconciliation after %s (%d)", headReleaseReconcileEvery, runs)
	}
	_ = p.run(ctx) // a bus kick always runs
	if runs != 4 {
		t.Fatalf("a kick did not run the pass (%d)", runs)
	}
}

// 2026-11-16 21:06: a kick came before main's CI verdict; with the loop at
// 5 minutes the STG release waited 5 minutes. After a kick the loop keeps
// 30-second ticks for headReleaseHotAfterKick.
func TestHeadReleaseLoopStaysHotAfterAKick(t *testing.T) {
	now := time.Date(2026, 11, 16, 21, 6, 13, 0, time.UTC)
	runs := 0
	p := &serialPass{pass: func(context.Context) error { runs++; return nil },
		live: func() bool { return true }, now: func() time.Time { return now }}
	p.hotUntil = now.Add(headReleaseHotAfterKick) // what runKicks sets
	_ = p.run(context.Background())               // the kick's pass
	now = now.Add(30 * time.Second)
	_ = p.tick(context.Background())
	if runs != 2 {
		t.Fatalf("30 s after a kick the loop did not pass (%d)", runs)
	}
	now = now.Add(headReleaseHotAfterKick)
	_ = p.tick(context.Background()) // last pass 10 min ago: reconcile anyway
	now = now.Add(30 * time.Second)
	_ = p.tick(context.Background())
	if runs != 3 {
		t.Fatalf("after the hot window a 30-second tick still passed (%d)", runs)
	}
}

// 2026-11-16: bus step 2 — pipeline records woken by ci.run.finished:
// gate-cluster's OnFinished queues the run for the controller and returns at
// once, also when the queue is full (that run is left to the reconciliation,
// which counts it missed); the controller is wired to the queue and to
// whether gate-cluster watches.
func TestRunFinishedQueuesWithoutBlocking(t *testing.T) {
	var b busWiring
	q := b.finishedRuns()
	done := make(chan struct{})
	go func() {
		for i := 0; i < cap(q)+10; i++ {
			b.runFinished(fmt.Sprintf("run-%d", i), "bifrost-ci-platform")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("OnFinished blocked on a full queue")
	}
	if len(q) != cap(q) || <-q != "run-0" {
		t.Fatalf("queue %d of %d", len(q), cap(q))
	}
	s := &Server{}
	c := &controller.Controller{}
	s.wirePipelineRecords(c)
	if c.PipelineFinished == nil || c.PipelineLive == nil || c.PipelineLive() {
		t.Fatal("the controller is not wired to gate-cluster's queue, or is live without it")
	}
	s.bus.recordsLive.Store(true)
	if !c.PipelineLive() {
		t.Fatal("live is not read from the bus wiring")
	}
}
