//bifrost:owner plan

package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/storekit"
)

// Old cards remain readable even though their creation path is retired.
func historicalMissionCard(t *testing.T, w *world, id, question string) (string, string) {
	t.Helper()
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := storekit.CreateMission(ctx, tx, "controller", storekit.Mission{ID: id, Title: "historical mission", Goal: "g", Deadline: time.Now().Add(time.Hour)}); err != nil {
			return err
		}
		if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "mission:" + id, Type: "mission_state_changed", Actor: "controller", MissionID: id,
			Body: map[string]any{"from": "proposed", "to": "awaiting_owner", "question_id": question, "reason": "historical card"}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.mission SET state='awaiting_owner', version=version+1 WHERE mission_id=$1`, id)
		return err
	}))
	return id, question
}

// Review 190 B4: plan changes and missions share needs_you's one bound.
//
// Serial: it lowers needs_you's bound (LimitPendingChangesForTest).
func TestNeedsYouOneBoundForEveryCard(t *testing.T) {
	w := newKernelWorld(t)
	historicalMissionCard(t, w, "M-30", "q_old_a")
	_, err := planstore.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "c", root, "rename the item", []planstore.PlanChange{{Op: "retitle", Node: item, Title: "x"}},
		func(context.Context, planstore.Card) (string, error) { return "q_plan", nil })
	ok(t, err)
	defer store.LimitPendingChangesForTest(1)()
	doc := w.progress(t)
	if doc.NeedsYou.Count != 1 || !doc.NeedsYou.Truncated {
		t.Fatalf("needs you at a bound of 1: %d, truncated %v", doc.NeedsYou.Count, doc.NeedsYou.Truncated)
	}
}
