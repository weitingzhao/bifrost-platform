package store_test

import (
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Review 464 C1: an attempt whose host never polled (imported rows) says its
// result is not persisted too.
func TestAHostThatNeverPolledSaysTheResultIsNotPersisted(t *testing.T) {
	t.Parallel()
	now := time.Now()
	status, reason := store.TaskStatus(store.StatusInput{TaskState: "running", Now: now,
		Attempt: store.LiveForTest("a_1", "stale", true, nil, nil, now.Add(-time.Minute))})
	if status != store.StatusHostLost || reason != "host h1 has never polled; result not persisted (no checkpoint)" {
		t.Fatalf("%s %q", status, reason)
	}
}
