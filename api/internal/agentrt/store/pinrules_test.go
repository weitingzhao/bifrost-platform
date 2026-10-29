package store_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Review 686 R-01: a pinned proposal and a rule write that takes the pin
// away (the entry dropped, or a version naming another proposer) are
// serialized on the rules' lock: the proposal holds it shared from before it
// reads the envelope to its commit, a rule write exclusively. Whichever takes
// it first commits first, and the other sees what it did: a pin proposed
// before the tightening is there before it, and one proposed after it is
// refused. Before the fix the rule write went through while the proposal
// waited, and the pin became visible after the authority was gone.

// pause holds the first hook at point until release is called, and says
// when it is held. A test that fails first releases it as it ends, so the
// writer it holds ends too.
func pause(t *testing.T, point string) (held <-chan struct{}, release func()) {
	h, r := make(chan struct{}), make(chan struct{})
	var first, done sync.Once
	unhook := store.SetHook(func(p, _ string) {
		if p != point {
			return
		}
		first.Do(func() {
			close(h)
			<-r
		})
	})
	release = func() { done.Do(func() { close(r) }) }
	t.Cleanup(func() { release(); unhook() })
	return h, release
}

// Serial: it holds the package's test hook (SetHook).
func TestPinProposalBeforeTheTighteningCommitsFirst(t *testing.T) {
	w := pinWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	held, release := pause(t, "proposed")
	proposed := make(chan error, 1)
	go func() {
		_, err := store.ProposePinnedTasks(ctx, pool, coordinator, "k", "M-1", "", []store.PinnedTask{w.pinnedTask("a", rules.PinCoordinator)})
		proposed <- err
	}()
	<-held
	tightened := make(chan error, 1)
	go func() {
		_, err := store.TightenRule(ctx, pool, wr("thread:s1"), "standing-envelope:dev", []byte(pinEnvelope(2, rules.Tighten, false)))
		tightened <- err
	}()
	// The tightening waits for the proposal holding the rules' lock.
	w.waitFor(t, "SELECT pg_advisory_xact_lock($1)", time.Now().Add(10*time.Second))
	select {
	case err := <-tightened:
		t.Fatalf("the tightening committed while the pinned proposal had not: %v", err)
	default:
	}
	release()
	ok(t, <-proposed)
	ok(t, <-tightened)
	var pinAt, ruleAt int64
	ok(t, w.rt.QueryRow(ctx, `SELECT max(event_id) FILTER (WHERE event_type = 'task_proposed' AND body_json ? 'executor_pin'),
	    max(event_id) FILTER (WHERE event_type IN ('rule_tightened','rule_signed','rule_proposed') AND event_stream_id LIKE '%standing-envelope:dev%')
	  FROM agentrt.event`).Scan(&pinAt, &ruleAt))
	if !(pinAt < ruleAt) {
		t.Fatalf("the pin (event %d) is not before the tightening (event %d)", pinAt, ruleAt)
	}
	if _, err := store.ProposePinnedTasks(ctx, w.rt, coordinator, "k2", "M-1", "", []store.PinnedTask{w.pinnedTask("a", rules.PinCoordinator)}); !errors.Is(err, store.ErrPinNotPermitted) {
		t.Fatalf("a pin after the tightening: %v", err)
	}
}

// Serial: it holds the package's test hook (SetHook).
func TestPinProposalAfterTheTighteningIsRefused(t *testing.T) {
	w := pinWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	held, release := pause(t, "rule locked")
	tightened := make(chan error, 1)
	go func() {
		_, err := store.TightenRule(ctx, pool, wr("thread:s1"), "standing-envelope:dev", []byte(pinEnvelope(2, rules.Tighten, false)))
		tightened <- err
	}()
	<-held
	proposed := make(chan error, 1)
	go func() {
		_, err := store.ProposePinnedTasks(ctx, pool, coordinator, "k", "M-1", "", []store.PinnedTask{w.pinnedTask("a", rules.PinCoordinator)})
		proposed <- err
	}()
	// The proposal waits for the tightening holding the rules' lock.
	w.waitFor(t, "SELECT pg_advisory_xact_lock_shared($1)", time.Now().Add(10*time.Second))
	release()
	ok(t, <-tightened)
	if err := <-proposed; !errors.Is(err, store.ErrPinNotPermitted) {
		t.Fatalf("a pin proposed while the tightening held the rules: %v", err)
	}
	if n := w.count(t, `SELECT count(*) FROM agentrt.task WHERE executor_pin IS NOT NULL`); n != 0 {
		t.Fatalf("%d pinned tasks", n)
	}
}
