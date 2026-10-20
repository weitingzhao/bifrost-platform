package store_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// keptNothing fails the test if a refused result of the attempt was written:
// persisted_at and pushed_commit stay NULL, no persist or salvage event, and
// the idempotency key of the route keeps no answer.
func (w *world) keptNothing(t *testing.T, attempt, host, route string) {
	t.Helper()
	var unbound bool
	ok(t, w.rt.QueryRow(ctx, `SELECT persisted_at IS NULL AND pushed_commit IS NULL FROM agentrt.attempt WHERE attempt_id = $1`,
		attempt).Scan(&unbound))
	if !unbound {
		t.Fatal("a refused result was bound to the attempt")
	}
	if p, s := w.events(t, "attempt_persisted", attempt), w.events(t, "attempt_salvaged", attempt); p != 0 || s != 0 {
		t.Fatalf("%d persisted and %d salvaged events for a refused result", p, s)
	}
	var keys int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.idempotency_key WHERE idempotency_key_id = $1`,
		"host:"+host+"|"+route+"|"+attempt).Scan(&keys))
	if keys != 0 {
		t.Fatalf("the %s key kept an answer for a refused result", route)
	}
}

// TD-360: a branch implementation whose pushed commit is its task's base
// changed nothing, and persist refuses it whatever agentd hands in, even
// when the branch really is at that commit. The same attempt with a commit
// of its own is taken.
func TestPersistRefusesTheBase(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	task := w.ready(t, store.Task{BaseCommit: commitA})
	c := w.claim(t, "h1", store.LeaseTTL, codex)
	if c == nil || c.TaskID != task {
		t.Fatalf("claim = %+v", c)
	}
	f := fence(c, "h1")
	ok(t, store.Start(ctx, w.rt, f, "", ""))
	exit := 0
	_, err := store.Computed(ctx, w.rt, f, &exit, "exit", "done")
	ok(t, err)

	report := w.artifact(t, "report", "# nothing to do\n")
	calls := 0
	_, err = store.Persist(ctx, w.rt, f, store.Result{Report: report, Ref: c.PushedRef, Commit: commitA},
		heads(map[string]string{c.PushedRef: commitA}, &calls))
	is(t, err, store.ErrState)
	if !strings.Contains(err.Error(), "changed nothing") || !strings.Contains(err.Error(), commitA) {
		t.Fatalf("the refusal says %q", err)
	}
	w.keptNothing(t, c.AttemptID, "h1", "persist")
	_, err = store.Submit(ctx, w.rt, f)
	is(t, err, store.ErrState)

	got, err := store.Persist(ctx, w.rt, f, store.Result{Report: report, Ref: c.PushedRef, Commit: commitB},
		heads(map[string]string{c.PushedRef: commitB}, &calls))
	ok(t, err)
	if got["persisted"] != true || w.events(t, "attempt_persisted", c.AttemptID) != 1 {
		t.Fatalf("persist = %v; %d persisted events", got, w.events(t, "attempt_persisted", c.AttemptID))
	}
	_, err = store.Submit(ctx, w.rt, f)
	ok(t, err)
	w.chainHolds(t, "mission:M-1")
}

// TD-360, salvage half: a lost attempt's result whose commit is the task's
// base is refused as persist refuses it, so no salvage check is proposed.
func TestSalvageRefusesTheBase(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	task := w.ready(t, store.Task{BaseCommit: commitA})
	c := w.claim(t, "h1", -store.Grace-time.Second, codex)
	if c == nil || c.TaskID != task {
		t.Fatalf("claim = %+v", c)
	}
	_, _, err := store.SweepLeases(ctx, w.rt)
	ok(t, err)
	if w.state(t, "attempt", c.AttemptID) != "lost" {
		t.Fatalf("attempt %s after the sweep", w.state(t, "attempt", c.AttemptID))
	}

	calls := 0
	_, err = store.Salvage(ctx, w.rt, c.AttemptID, "h1", store.Result{Report: w.artifact(t, "report", "# salvaged\n"),
		Ref: c.PushedRef, Commit: commitA}, heads(map[string]string{c.PushedRef: commitA}, &calls))
	is(t, err, store.ErrState)
	if !strings.Contains(err.Error(), "changed nothing") {
		t.Fatalf("the refusal says %q", err)
	}
	w.keptNothing(t, c.AttemptID, "h1", "salvage")
	pending, err := store.PendingSalvaged(ctx, w.rt)
	ok(t, err)
	if slices.Contains(pending, c.AttemptID) {
		t.Fatalf("a refused salvage is pending its check: %v", pending)
	}
	w.chainHolds(t, "mission:M-1")
}
