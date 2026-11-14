package store_test

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// prepFail leases task to tl on h1 and ends the attempt as agentd ends one
// whose preparation failed (agentd prepare.go): started, a checkpoint naming
// its report (none when report is ""), computed and ended prepare_failed.
func (w *world) prepFail(t *testing.T, task string, tl store.Tool, report string) *store.Claim {
	t.Helper()
	c := w.claim(t, "h1", store.LeaseTTL, tl)
	if c == nil || c.TaskID != task {
		t.Fatalf("%s was not leased the task at once: %+v", tl.Executor, c)
	}
	f := fence(c, "h1")
	ok(t, store.Start(ctx, w.rt, f, "", ""))
	if report != "" {
		_, err := store.Checkpoint(ctx, w.rt, f, 1, []string{w.artifact(t, "report", report)}, false)
		ok(t, err)
	}
	exit := 3
	_, err := store.Computed(ctx, w.rt, f, &exit, "prepare_failed", "failed")
	ok(t, err)
	_, err = store.Ended(ctx, w.rt, c.AttemptID, "h1", "prepare_failed", true)
	ok(t, err)
	return c
}
