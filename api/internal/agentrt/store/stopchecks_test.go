package store_test

import (
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// T09: a stop on a task under verification stops its check too: the check
// is not claimed while the stop holds, and a check already running is told to
// stop at its next heartbeat. Lifted, the check goes on.
func TestAStopReachesTheTasksChecks(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	impl := w.ready(t, store.Task{})
	w.run(t, "h1", cursor, "done")
	v, err := store.StartVerification(ctx, w.rt, impl)
	ok(t, err)
	first := store.NewControlID()
	w.control(t, first, "task", impl, "stop")
	if c := w.claim(t, "h2", store.LeaseTTL, deterministic); c != nil {
		t.Fatalf("the check of a stopped task was claimed: %+v", c)
	}
	w.clear(t, first)
	c := w.claim(t, "h2", store.LeaseTTL, deterministic)
	if c == nil || c.TaskID != v {
		t.Fatalf("the check after the lift = %+v", c)
	}
	f := fence(c, "h2")
	ok(t, store.Start(ctx, w.rt, f, "", ""))
	if b, err := store.Heartbeat(ctx, w.rt, f); err != nil || b.Stop {
		t.Fatalf("a heartbeat before the stop: %+v %v", b, err)
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		return store.SetControl(ctx, tx, "owner", store.NewControlID(), "task", impl, "stop", "test")
	}))
	if b, err := store.Heartbeat(ctx, w.rt, f); err != nil || !b.Stop {
		t.Fatalf("the running check's heartbeat after its subject's stop: %+v %v", b, err)
	}
}
