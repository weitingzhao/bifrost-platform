package store

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A test's own event clock. A test that moves time gives itself one
// (ownClock) and calls the store with the context that carries it; it
// freezes that clock (freezeClock), not the process's (FreezeEventClock), so
// it runs while its neighbours run, each at its own time. The helpers a test
// calls ask for its context by the test (ctxOf): the test's own when it has
// a clock, a plain one otherwise, so one helper serves both kinds of test.

// testClock reads the wall clock until it is frozen.
type testClock struct{ at atomic.Pointer[time.Time] }

func (c *testClock) now() time.Time {
	if at := c.at.Load(); at != nil {
		return *at
	}
	return time.Now()
}

type ownTime struct {
	clock *testClock
	ctx   context.Context
}

// ownTimes holds each running test's clock under the test's name: a key per
// test, written when the test starts and removed when it ends, so tests
// running at once do not meet in it.
var ownTimes sync.Map

// ownTimeOf is the clock of t: its own, or the one of the test it is a
// subtest of (nearest first); nil when neither has one.
func ownTimeOf(t testing.TB) *ownTime {
	for name := t.Name(); ; {
		if o, ok := ownTimes.Load(name); ok {
			return o.(*ownTime)
		}
		i := strings.LastIndex(name, "/")
		if i < 0 {
			return nil
		}
		name = name[:i]
	}
}

// ownClock gives t an event clock of its own, the wall clock until t freezes
// it, and returns the context that carries it. A test calls it first (after
// t.Parallel()) and names the result ctx, which hides the package's ctx from
// the rest of its body; its subtests and the helpers it calls with its t
// find the same context (ctxOf).
func ownClock(t testing.TB) context.Context {
	t.Helper()
	c := &testClock{}
	o := &ownTime{clock: c, ctx: WithEventClock(context.Background(), c.now)}
	name := t.Name()
	if _, dup := ownTimes.LoadOrStore(name, o); dup {
		t.Fatalf("%s already has an event clock of its own", name)
	}
	// Registered first, so it runs after every cleanup the test adds: those
	// still read the test's clock.
	t.Cleanup(func() { ownTimes.Delete(name) })
	return o.ctx
}

// ctxOf is the context a helper calls the store with on behalf of t: the one
// carrying t's own event clock when it has one, a plain one otherwise.
func ctxOf(t testing.TB) context.Context {
	if o := ownTimeOf(t); o != nil {
		return o.ctx
	}
	return context.Background()
}

// freezeClock makes t's own event clock read at, as FreezeEventClock does
// for the process's; the function it returns gives the wall clock back. A
// test with no clock of its own fails here: its store calls would not read
// the frozen time.
func freezeClock(t testing.TB, at time.Time) func() {
	t.Helper()
	o := ownTimeOf(t)
	if o == nil {
		t.Fatalf("%s has no event clock of its own: call ownClock(t) first", t.Name())
		return nil
	}
	o.clock.at.Store(&at)
	return func() { o.clock.at.Store(nil) }
}

// The same three for the tests outside the package (store_test).
var (
	OwnClock    = ownClock
	CtxOf       = ctxOf
	FreezeClock = freezeClock
)
