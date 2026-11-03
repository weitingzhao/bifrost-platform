package store_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// R5:5.5-19 §5: the explanation cache. A request never waits; one
// computation at a time; 30 seconds of reuse; 60 seconds of backoff after a
// failure; the last success kept through a failure; eight roots at most.

// cacheClock is a settable clock.
type cacheClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *cacheClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.at }
func (c *cacheClock) add(d time.Duration) {
	c.mu.Lock()
	c.at = c.at.Add(d)
	c.mu.Unlock()
}

// cacheRun is one computation the test lets finish.
type cacheRun struct {
	root string
	done chan error
}

// cacheFake hands each computation to the test and records how many run at
// once.
type cacheFake struct {
	clk           *cacheClock
	runs          chan cacheRun
	running, peak atomic.Int32
	calls         atomic.Int32
}

func newCacheFake(clk *cacheClock) *cacheFake {
	return &cacheFake{clk: clk, runs: make(chan cacheRun, 16)}
}

func (f *cacheFake) compute(_ context.Context, _ store.DB, root string) (map[string]any, time.Time, error) {
	f.calls.Add(1)
	n := f.running.Add(1)
	defer f.running.Add(-1)
	for {
		p := f.peak.Load()
		if n <= p || f.peak.CompareAndSwap(p, n) {
			break
		}
	}
	r := cacheRun{root: root, done: make(chan error)}
	f.runs <- r
	if err := <-r.done; err != nil {
		return nil, time.Time{}, err
	}
	return map[string]any{root + ":1": map[string]any{"kind": "no_done_rule", "node_version": 1}}, f.clk.now(), nil
}

// none fails if a computation starts within a while: a Get that must not
// start one is shown not to, not only not to have started one yet.
func (f *cacheFake) none(t *testing.T, why string) {
	t.Helper()
	select {
	case r := <-f.runs:
		t.Fatalf("%s: a computation of %s started", why, r.root)
	case <-time.After(150 * time.Millisecond):
	}
}

// next is the computation started next, failing after a while.
func (f *cacheFake) next(t *testing.T) cacheRun {
	t.Helper()
	select {
	case r := <-f.runs:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("no computation started")
	}
	return cacheRun{}
}

// settle waits until the cache has taken in the last result.
func settle(t *testing.T, c *store.ExplainCache, root, want string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		b := c.Get(root)
		if b["status"] == want || time.Now().After(deadline) {
			return b
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func newCache(clk *cacheClock, f *cacheFake) *store.ExplainCache {
	return &store.ExplainCache{Now: clk.now, Compute: f.compute}
}

// The first request is pending at once; the result comes from the
// background; it is reused for 30 seconds, then computed again.
func TestExplainCachePendingThenOK(t *testing.T) {
	t.Parallel()
	clk := &cacheClock{at: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)}
	f := newCacheFake(clk)
	c := newCache(clk, f)
	if b := c.Get("R1"); b["status"] != "pending" || b["read_at"] != nil || len(b["steps"].(map[string]any)) != 0 {
		t.Fatalf("first request: %v", b)
	}
	f.next(t).done <- nil
	b := settle(t, c, "R1", "ok")
	if b["status"] != "ok" || b["read_at"] == nil || b["last_error"] != nil || len(b["steps"].(map[string]any)) != 1 {
		t.Fatalf("after the computation: %v", b)
	}
	clk.add(29 * time.Second)
	c.Get("R1")
	f.none(t, "29 s after a success")
	clk.add(time.Second)
	if b := c.Get("R1"); b["status"] != "ok" {
		t.Fatalf("a stale result is still the last success: %v", b)
	}
	f.next(t).done <- nil
	if f.calls.Load() != 2 {
		t.Fatalf("not recomputed after 30 seconds: %d", f.calls.Load())
	}
}

// A root that never succeeded and fails reads failed with the contract's
// reason, never the error's text; it is not tried again for 60 seconds. A
// later failure keeps the earlier success, with last_error.
func TestExplainCacheFailureAndBackoff(t *testing.T) {
	t.Parallel()
	clk := &cacheClock{at: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)}
	f := newCacheFake(clk)
	c := newCache(clk, f)
	c.Get("R1")
	f.next(t).done <- &store.ErrExplainIncomplete{Reason: "input_too_large", Which: "rows"}
	b := settle(t, c, "R1", "failed")
	if b["status"] != "failed" || b["reason"] != "input_too_large" || b["read_at"] != nil || b["last_attempt_at"] == nil ||
		len(b["steps"].(map[string]any)) != 0 {
		t.Fatalf("failed cold: %v", b)
	}
	clk.add(59 * time.Second)
	c.Get("R1")
	f.none(t, "59 s after a failure")
	clk.add(time.Second)
	c.Get("R1")
	f.next(t).done <- nil
	settle(t, c, "R1", "ok")
	clk.add(31 * time.Second)
	c.Get("R1")
	f.next(t).done <- errors.New("SELECT secret FROM somewhere: boom")
	deadline := time.Now().Add(5 * time.Second)
	for b = c.Get("R1"); b["last_error"] == nil && time.Now().Before(deadline); b = c.Get("R1") {
		time.Sleep(5 * time.Millisecond)
	}
	if b["status"] != "ok" || b["read_at"] == nil || b["last_error"] != "error" || len(b["steps"].(map[string]any)) != 1 {
		t.Fatalf("an old success and a failure: %v", b)
	}
}

// Two roots asked together: one computation at a time, the second after
// the first, none left waiting.
func TestExplainCacheOneComputationAtATime(t *testing.T) {
	t.Parallel()
	clk := &cacheClock{at: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)}
	f := newCacheFake(clk)
	c := newCache(clk, f)
	c.Get("R1")
	c.Get("R2")
	c.Get("R1") // asked again while queued or running: not twice
	first := f.next(t)
	select {
	case r := <-f.runs:
		t.Fatalf("a second computation started beside %s: %s", first.root, r.root)
	case <-time.After(50 * time.Millisecond):
	}
	first.done <- nil
	second := f.next(t)
	if second.root == first.root {
		t.Fatalf("the same root twice: %s", first.root)
	}
	second.done <- nil
	settle(t, c, "R1", "ok")
	settle(t, c, "R2", "ok")
	if f.peak.Load() != 1 || f.calls.Load() != 2 {
		t.Fatalf("peak %d, calls %d; want 1, 2", f.peak.Load(), f.calls.Load())
	}
}

// Eight roots are kept: a ninth drops the least recently used, which reads
// pending again.
func TestExplainCacheKeepsEightRoots(t *testing.T) {
	t.Parallel()
	clk := &cacheClock{at: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)}
	f := newCacheFake(clk)
	c := newCache(clk, f)
	for i := 1; i <= 9; i++ {
		root := fmt.Sprintf("R%d", i)
		c.Get(root)
		f.next(t).done <- nil
		settle(t, c, root, "ok")
		clk.add(time.Second)
	}
	if b := c.Get("R1"); b["status"] != "pending" {
		t.Fatalf("the least recently used root was kept: %v", b)
	}
	f.next(t).done <- nil
	for i := 3; i <= 9; i++ {
		if b := c.Get(fmt.Sprintf("R%d", i)); b["status"] != "ok" {
			t.Fatalf("R%d dropped: %v", i, b)
		}
	}
}

// A computation that panics fails the root, not the process.
func TestExplainCachePanicIsAFailure(t *testing.T) {
	t.Parallel()
	c := &store.ExplainCache{Compute: func(context.Context, store.DB, string) (map[string]any, time.Time, error) { panic("boom") }}
	c.Get("R1")
	if b := settle(t, c, "R1", "failed"); b["status"] != "failed" || b["reason"] != "error" {
		t.Fatalf("after a panic: %v", b)
	}
}

// Review round 11 M1: roots asked while the one computation runs, one of
// them dropped from the cache and asked again, are each computed once: a
// dropped root leaves the queue, so it neither runs twice nor grows it.
func TestExplainCacheEvictedWhileQueued(t *testing.T) {
	t.Parallel()
	clk := &cacheClock{at: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)}
	f := newCacheFake(clk)
	c := newCache(clk, f)
	c.Get("R0")
	hold := f.next(t) // R0 holds the slot
	for i := 1; i <= 9; i++ {
		c.Get(fmt.Sprintf("R%d", i)) // R1 and R2 are dropped as R8 and R9 come in
		clk.add(time.Second)
	}
	c.Get("R1") // asked again: queued once more, and R3 dropped for it
	hold.done <- nil
	seen := map[string]int{}
	for {
		select {
		case r := <-f.runs:
			seen[r.root]++
			r.done <- nil
			continue
		case <-time.After(300 * time.Millisecond):
		}
		break
	}
	for root, n := range seen {
		if n != 1 {
			t.Errorf("%s computed %d times", root, n)
		}
	}
	if seen["R1"] != 1 || seen["R2"] != 0 || seen["R3"] != 0 || len(seen) != 7 {
		t.Fatalf("computed %v; want R1 and R4..R9 once each, the dropped R2 and R3 not at all", seen)
	}
}

// And a dropped root that failed is backed off again only as itself: asked
// back, it is a new entry and computes once.
func TestExplainCacheEvictedAfterFailure(t *testing.T) {
	t.Parallel()
	clk := &cacheClock{at: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)}
	f := newCacheFake(clk)
	c := newCache(clk, f)
	c.Get("R1")
	f.next(t).done <- errors.New("boom")
	settle(t, c, "R1", "failed")
	c.Get("R1")
	f.none(t, "backing off")
	for i := 2; i <= 9; i++ { // R1 dropped
		c.Get(fmt.Sprintf("R%d", i))
		f.next(t).done <- nil
		clk.add(time.Second)
	}
	c.Get("R1")
	r := f.next(t)
	if r.root != "R1" {
		t.Fatalf("started %s", r.root)
	}
	r.done <- nil
	f.none(t, "R1 computed once")
}
