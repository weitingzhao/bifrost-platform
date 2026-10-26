package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

func failLandingBound(ctx context.Context, tx pgx.Tx, op string) error {
	_, err := Append(ctx, tx, Event{Stream: "landing:bifrost-platform", Type: "note", Actor: "controller", Body: map[string]any{
		"text": "STG landing drill: bound", "landing": map[string]any{"v": landing.Version, "kind": "failed", "op": op, "why": "bound"},
	}})
	return err
}

// The repository stream must be locked. Only the latest discarded operation
// can acquire a born-terminal successor; repeated recovery is a no-op.
func failDiscardedBound(ctx context.Context, tx pgx.Tx, state landing.State, task string, now time.Time) error {
	first, n := landing.RootBudget(state, task)
	if state.Operations[state.LatestOperation(task)].Phase != "discarded" || !landing.Bound(first, n, now) {
		return nil
	}
	return failLandingBound(ctx, tx, fmt.Sprintf("land:%s:%d", task, n+1))
}

// SettleLandingDrillBounds completes all database-only recovery before any
// external read. No current rule or available Git/CI source is required to
// release an expired slot or give a discarded root its retryable ending.
func SettleLandingDrillBounds(ctx context.Context, db DB, now time.Time) (state landing.State, err error) {
	err = InTx(ctx, db, func(tx pgx.Tx) error {
		if e := lockStream(ctx, tx, "landing:bifrost-platform"); e != nil {
			return e
		}
		var e error
		state, e = LandingState(ctx, tx, "landing:bifrost-platform")
		if e != nil {
			return e
		}
		ids := make([]string, 0, len(state.Operations))
		for id := range state.Operations {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			o := state.Operations[id]
			if o.Task == "" {
				continue
			}
			first, n := landing.RootBudget(state, o.Task)
			if o.Phase == "discarded" && state.LatestOperation(o.Task) == id {
				e = failDiscardedBound(ctx, tx, state, o.Task, now)
			} else if !o.Terminal() && landing.Bound(first, n-1, now) {
				e = failLandingBound(ctx, tx, id)
			}
			if e != nil {
				return e
			}
		}
		state, e = LandingState(ctx, tx, "landing:bifrost-platform")
		return e
	})
	return
}
