package store_test

import (
	"sync"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// Regression tests for review 188.

// heldControl starts a control by a thread that has never written (its
// first write registers it, holding the registration lock and the system
// stream) and holds it after its writer is ensured, before it takes the host
// row. It returns when the control is held, with the function that lets it
// go and the channel of its outcome.
func heldControl(t *testing.T, w *world, actor string) (func(), <-chan error) {
	t.Helper()
	pool := w.db.Pool(t, w.db.Runtime)
	entered, release := make(chan struct{}), make(chan struct{})
	var once, freed sync.Once
	unhook := store.SetHook(func(point, who string) {
		if point == "control writer" && who == actor {
			once.Do(func() { close(entered); <-release })
		}
	})
	free := func() { freed.Do(func() { close(release) }) }
	t.Cleanup(func() { free(); unhook() })
	out := make(chan error, 1)
	go func() {
		_, err := store.RequestControl(ctx, pool, wr(actor), "drain-h1", "host", "h1", "drain", "test")
		out <- err
	}()
	<-entered
	return free, out
}

// within reports what ch gives within d, or false.
func within[T any](ch <-chan T, d time.Duration) (T, bool) {
	select {
	case v := <-ch:
		return v, true
	case <-time.After(d):
		var zero T
		return zero, false
	}
}

// R1: while a new thread's control holds the system stream, a rule write by
// a registered writer does not wait for it (its events go to the rule's own
// stream), so a claim on the control's host finishes too; then the control.
//
// Serial: it holds the package's test hook (SetHook).
func TestRuleWriteDoesNotWaitForARegistration(t *testing.T) {
	w := newKernelWorld(t)
	s := signer{t, rptest.NewKey(t)}
	ok(t, firstErr(s.sign(w, jobText(1, rules.Initial, candidate("exec:codex", "openai", "active")))))
	task := w.ready(t, store.Task{})
	pool := w.db.Pool(t, w.db.Runtime)
	free, controlled := heldControl(t, w, "thread:new1")
	tightened := make(chan error, 1)
	go func() {
		// A demotion, so the candidate's status change is written too.
		_, err := store.TightenRule(ctx, pool, store.Writer{Actor: "owner", Token: "admin"}, "job:implementer",
			[]byte(jobText(2, rules.Tighten, candidate("exec:codex", "openai", "probation"))))
		tightened <- err
	}()
	err, done := within(tightened, 5*time.Second)
	if !done {
		free()
		t.Fatal("the rule write waited for the new thread's control")
	}
	ok(t, err)
	got, done := within(claimNow(t, pool, "h1"), 5*time.Second)
	free()
	if !done {
		t.Fatal("the claim waited")
	}
	ok(t, got.err)
	if got.c == nil || got.c.TaskID != task {
		t.Fatalf("the claim: %+v; want %s", got.c, task)
	}
	ok(t, <-controlled)
}

// R1: a rule write by another new thread waits for the registration lock
// before it takes the capability lock, so a claim does not wait behind it.
//
// Serial: it holds the package's test hook (SetHook).
func TestRuleWriteRegistersBeforeTheCapabilityLock(t *testing.T) {
	w := newKernelWorld(t)
	s := signer{t, rptest.NewKey(t)}
	ok(t, firstErr(s.sign(w, jobText(1, rules.Initial, candidate("exec:codex", "openai", "active")))))
	task := w.ready(t, store.Task{})
	pool := w.db.Pool(t, w.db.Runtime)
	free, controlled := heldControl(t, w, "thread:new1")
	tightened := make(chan error, 1)
	go func() {
		_, err := store.TightenRule(ctx, pool, wr("thread:new2"), "job:implementer",
			[]byte(jobText(2, rules.Tighten, candidate("exec:codex", "openai", "active"))))
		tightened <- err
	}()
	w.waitFor(t, "pg_advisory_xact_lock(742109)", time.Now().Add(10*time.Second))
	got, done := within(claimNow(t, pool, "h1"), 5*time.Second)
	free()
	if !done {
		t.Fatal("the claim waited behind the new thread's rule write")
	}
	ok(t, got.err)
	if got.c == nil || got.c.TaskID != task {
		t.Fatalf("the claim: %+v; want %s", got.c, task)
	}
	ok(t, <-controlled)
	ok(t, <-tightened)
}

// claimNow claims on host with codex, in the background.
func claimNow(t *testing.T, pool store.DB, host string) <-chan claimed {
	t.Helper()
	out := make(chan claimed, 1)
	go func() {
		c, err := store.ClaimFor(ctx, pool, store.ClaimRequest{HostID: host, FreeSlots: 1, Tools: []store.Tool{codex}}, time.Hour)
		out <- claimed{c, err}
	}()
	return out
}
