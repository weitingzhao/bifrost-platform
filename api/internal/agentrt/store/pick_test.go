package store_test

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// choice is what the attempt_leased event of a claim records of its pick.
func (w *world) choice(t *testing.T, c *store.Claim) map[string]any {
	t.Helper()
	var body map[string]any
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json FROM agentrt.event WHERE event_type = 'attempt_leased' AND attempt_id = $1`,
		c.AttemptID).Scan(&body))
	ch, _ := body["choice"].(map[string]any)
	return ch
}

// ⑦: with nothing read, the executor that took work longest ago goes first.
func TestPickRotates(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.ready(t, store.Task{})
	w.ready(t, store.Task{})
	first := w.claim(t, "h1", store.LeaseTTL, codex, claude)
	if first == nil || first.Tool.Executor != "exec:claude" || w.choice(t, first)["by"] != "route" {
		t.Fatalf("first = %+v", first)
	}
	second := w.claim(t, "h2", store.LeaseTTL, codex, claude)
	if second == nil || second.Tool.Executor != "exec:codex" || w.choice(t, second)["by"] != "rotation" {
		t.Fatalf("second = %+v (%v)", second, w.choice(t, second))
	}
}
