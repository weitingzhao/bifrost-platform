package store_test

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Tasks without a base must still lease: an empty optional string is invalid
// in the event vocabulary. A supplied base must match what the host receives.
func TestClaimBaseEvent(t *testing.T) {
	t.Parallel()
	for _, base := range []string{"", commitA} {
		t.Run(base, func(t *testing.T) {
			t.Parallel()
			w := newKernelWorld(t)
			w.ready(t, store.Task{BaseCommit: base})
			c := w.claim(t, "h1", store.LeaseTTL, codex)
			if c == nil {
				t.Fatal("no claim")
			}
			var body map[string]any
			ok(t, w.rt.QueryRow(ctx, `SELECT body_json FROM agentrt.event WHERE event_type='attempt_leased' AND attempt_id=$1`, c.AttemptID).Scan(&body))
			got, present := body["base_commit"]
			if c.BaseCommit != base || (base == "" && present) || (base != "" && got != base) {
				t.Fatalf("base=%q claim=%q event=%v", base, c.BaseCommit, body)
			}
		})
	}
}
