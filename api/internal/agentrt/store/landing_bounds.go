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
	return failLandingLimit(ctx, tx, op, "bound")
}

func failLandingLimit(ctx context.Context, tx pgx.Tx, op, why string) error {
	_, err := Append(ctx, tx, Event{Stream: "landing:bifrost-platform", Type: "note", Actor: "controller", Body: map[string]any{
		"text": "Landing: " + why, "landing": map[string]any{"v": landing.Version, "kind": "failed", "op": op, "why": why},
	}})
	return err
}

// SettleLandingOperationBound rechecks a preparer's timer decision under the
// repository stream lock. A phase, generation or record changed while it read
// external evidence makes the old decision a no-op. No main I/O occurs here.
func SettleLandingOperationBound(ctx context.Context, db DB, op string, observed landing.Operation) (why string, err error) {
	err = InTx(ctx, db, func(tx pgx.Tx) error {
		why = ""
		if err := lockStream(ctx, tx, "landing:bifrost-platform"); err != nil {
			return err
		}
		s, err := LandingState(ctx, tx, "landing:bifrost-platform")
		if err != nil {
			return err
		}
		o, ok := s.Operations[op]
		if !ok || !o.EndsAtBound() || s.LatestOperation(o.Task) != op || o.Task != observed.Task ||
			o.Phase != observed.Phase || !o.LastAt.Equal(observed.LastAt) {
			return nil
		}
		why = landing.RootBound(s, o.Task, DecisionAt(), false)
		if why == "" {
			return nil
		}
		return failLandingLimit(ctx, tx, op, why)
	})
	return
}

// The repository stream must be locked. Only a root whose latest operation
// awaits its successor (discarded, non_ff, retried; landing.AwaitsNext) can
// acquire a born-terminal successor; repeated recovery is a no-op.
func failAwaitingBound(ctx context.Context, tx pgx.Tx, state landing.State, task string, now time.Time) error {
	_, n := landing.RootBudget(state, task)
	why := landing.RootBound(state, task, now, true)
	if !state.AwaitsNext(task) || why == "" {
		return nil
	}
	return failLandingLimit(ctx, tx, fmt.Sprintf("land:%s:%d", task, n+1), why)
}

// SettleLandingDrillBounds completes all database-only recovery before any
// external read. No current rule or available Git/CI source is required to
// release an expired slot or give a discarded root its retryable ending.
func SettleLandingDrillBounds(ctx context.Context, db DB, now time.Time) (state landing.State, err error) {
	err = InTx(ctx, db, func(tx pgx.Tx) error {
		if e := lockStream(ctx, tx, "landing:bifrost-platform"); e != nil {
			return e
		}
		now := DecisionAt() // CUT §9: the clock its records are written with
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
			// An operation at main (main_intent, main_result other than
			// non_ff) is left for reconciliation: main_intent and
			// main_result{ok} refuse failed{bound} (one such append would
			// roll back every other root's settlement here), unknown may
			// take it only after a main read shows R absent, and denied ends
			// failed{denied} (TD-447 2, review 645 N1).
			if state.LatestOperation(o.Task) == id && state.AwaitsNext(o.Task) {
				e = failAwaitingBound(ctx, tx, state, o.Task, now)
			} else if why := landing.RootBound(state, o.Task, now, false); o.EndsAtBound() && why != "" {
				e = failLandingLimit(ctx, tx, id, why)
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
