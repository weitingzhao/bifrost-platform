package headrelease

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// 2026-11-16: bus step 1: the bus's kick runs head-release's own pass, one
// at a time with the controller's loop; with the switch off nothing is wrapped.
func TestHeadReleaseKickRunsTheSamePass(t *testing.T) {
	t.Parallel()
	var b Waker
	if b.Serial(nil) != nil {
		t.Fatal("an off switch became a pass")
	}
	var runs, inside atomic.Int32
	pass := b.Serial(func(context.Context) error {
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
	b.kickPass()
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
// loop became a 5-minute reconciliation; since 2026-11-18 (L04) that is its only
// cadence, the bus up or down.
func TestHeadReleaseLoopReconciles(t *testing.T) {
	now := time.Date(2026, 11, 16, 20, 0, 0, 0, time.UTC)
	runs := 0
	p := &serialPass{pass: func(context.Context) error { runs++; return nil }, now: func() time.Time { return now }}
	ctx := context.Background()
	_ = p.tick(ctx)
	if runs != 1 {
		t.Fatalf("the first tick did not run a pass (%d)", runs)
	}
	now = now.Add(30 * time.Second)
	_ = p.tick(ctx)
	if runs != 1 {
		t.Fatalf("a tick 30 s after a pass ran one (%d)", runs)
	}
	now = now.Add(reconcileEvery)
	_ = p.tick(ctx)
	if runs != 2 {
		t.Fatalf("no reconciliation after %s (%d)", reconcileEvery, runs)
	}
	_ = p.run(ctx) // a bus kick always runs
	if runs != 3 {
		t.Fatalf("a kick did not run the pass (%d)", runs)
	}
}

// 2026-11-16 21:06: a kick came before main's CI verdict; with the loop at
// 5 minutes the STG release waited 5 minutes. After a kick the loop keeps
// 30-second ticks for hotAfterKick.
func TestHeadReleaseLoopStaysHotAfterAKick(t *testing.T) {
	now := time.Date(2026, 11, 16, 21, 6, 13, 0, time.UTC)
	runs := 0
	p := &serialPass{pass: func(context.Context) error { runs++; return nil }, now: func() time.Time { return now }}
	p.hotUntil = now.Add(hotAfterKick) // what runKicks sets
	_ = p.run(context.Background())    // the kick's pass
	now = now.Add(30 * time.Second)
	_ = p.tick(context.Background())
	if runs != 2 {
		t.Fatalf("30 s after a kick the loop did not pass (%d)", runs)
	}
	now = now.Add(hotAfterKick)
	_ = p.tick(context.Background()) // last pass 10 min ago: reconcile anyway
	now = now.Add(30 * time.Second)
	_ = p.tick(context.Background())
	if runs != 3 {
		t.Fatalf("after the hot window a 30-second tick still passed (%d)", runs)
	}
}

// 2026-11-16 night: of 564 s from main to STG released, 267 s were waiting
// for the release window; a pass that found it held waited for the next
// 5-minute reconciliation. The window's change now wakes head-release too.
func TestHeadReleaseWakesOnTheWindow(t *testing.T) {
	want := map[string]bool{"git.main.advanced": false, "ci.run.finished": false, "release.window.changed": false}
	for _, typ := range wakeTypes {
		want[typ] = true
	}
	for typ, ok := range want {
		if !ok {
			t.Errorf("head-release is not woken by %s", typ)
		}
	}
}

// 2026-11-17, with the communication layer: the newest event worth a
// head-release pass is the cause a started run names; a start clears it only
// if no newer wake came meanwhile. Positive control: with clearWake storing
// "" unconditionally, the last check sees "" instead of ev-3.
func TestHeadReleaseRemembersTheNewestWake(t *testing.T) {
	var b Waker
	if b.LastWake() != "" {
		t.Fatal("no wake yet")
	}
	b.ClearWake("ev-1") // nothing kept: no panic, nothing changes
	b.noteWake("ev-1")
	b.noteWake("ev-2")
	if b.LastWake() != "ev-2" {
		t.Fatalf("lastWake = %q", b.LastWake())
	}
	b.ClearWake("ev-2")
	if b.LastWake() != "" {
		t.Fatalf("a run started with ev-2 left %q", b.LastWake())
	}
	b.noteWake("ev-3")
	b.ClearWake("ev-2") // a start that read ev-2 before ev-3 came
	if b.LastWake() != "ev-3" {
		t.Fatalf("a newer wake was lost: %q", b.LastWake())
	}
}
