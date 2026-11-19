package planstore_test

import (
	"context"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
)

// Review 190 B4: plan changes and missions share needs_you's one bound.
//
// Serial: it lowers needs_you's bound (LimitPendingChangesForTest).
func TestNeedsYouOneBoundForEveryCard(t *testing.T) {
	w := newKernelWorld(t)
	historicalMissionCard(t, w, "M-30", "q_old_a")
	_, err := planstore.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "c", root, "rename the item", []planstore.PlanChange{{Op: "retitle", Node: item, Title: "x"}},
		func(context.Context, planstore.Card) (string, error) { return "q_plan", nil })
	ok(t, err)
	defer limitPendingChanges(1)()
	doc := w.limitProgress(t)
	if doc.NeedsYou.Count != 1 || !doc.NeedsYou.Truncated {
		t.Fatalf("needs you at a bound of 1: %d, truncated %v", doc.NeedsYou.Count, doc.NeedsYou.Truncated)
	}
}
