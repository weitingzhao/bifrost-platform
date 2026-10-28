package store_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// TD-447 1, 2, 6 boundaries the first three tests missed (review 645, Codex;
// adopted by the coordinator).
// Requires the real PG/Redis harness.

// Serial: FreezeEventClock. The eighth operation ending non_ff within the
// hour still gets its failed{bound} successor (the count, not the clock).
func TestNonFFCountBoundSettlesWithinTheHour(t *testing.T) {
	w, task, _ := goalDrill(t, "M-3")
	at := time.Now().UTC().Truncate(time.Microsecond)
	defer store.FreezeEventClock(at)()
	for i := 1; i <= 8; i++ {
		prodChain(t, w, task, fmt.Sprintf("land:%s:%d", task, i), at.Add(time.Duration(i-1)*7*time.Minute), "main_result")
	}
	now := at.Add(time.Hour)
	store.FreezeEventClock(now)
	s, err := store.SettleLandingDrillBounds(ctx, w.rt, now)
	ok(t, err)
	next := fmt.Sprintf("land:%s:9", task)
	if s.LatestOperation(task) != next || s.Operations[next].Outcome != "bound" {
		t.Fatal("eighth non_ff left awaiting a forbidden ninth claim", s.LatestOperation(task))
	}
	closeEnds(t, w, "count-bound successor did not release mission")
}

// Serial: FreezeEventClock. main_result ok, denied and unknown hold their
// mission past the hold limit until reconciled (review 645 m3).
func TestMainResultHoldsUntilReconciled(t *testing.T) {
	for _, outcome := range []string{"ok", "denied", "unknown"} {
		t.Run(outcome, func(t *testing.T) {
			w, task, _ := goalDrill(t, "M-3")
			at := time.Now().UTC().Truncate(time.Microsecond)
			defer store.FreezeEventClock(at)()
			op := "land:" + task + ":1"
			prodChain(t, w, task, op, at, "main_intent")
			ok(t, appendLanding(w, landingRepoStream, landingRecord(op, "main_result", "outcome", outcome)))
			later := at.Add(7 * time.Hour)
			store.FreezeEventClock(later)
			s, err := store.SettleLandingDrillBounds(ctx, w.rt, later)
			ok(t, err)
			if s.Operations[op].Phase != "main_result" || s.Operations[op].Outcome != outcome {
				t.Fatal("settler rewrote main result")
			}
			closeKeeps(t, w, "unreconciled main_result lost mission")
			// Supply the protocol record a real reconciler would append.
			if outcome == "ok" {
				ok(t, appendLanding(w, landingRepoStream, landingRecord(op, "landed", "R", commitB)))
			} else {
				why := "bound"
				if outcome == "denied" {
					why = "denied"
				}
				ok(t, appendLanding(w, landingRepoStream, landingRecord(op, "failed", "why", why)))
			}
			closeEnds(t, w, "resolved result still holds")
		})
	}
}

type work645AfterHoldDB struct {
	store.DB
	after func() error
}
type work645AfterHoldTx struct {
	pgx.Tx
	after  func() error
	nested bool
}

func (d work645AfterHoldDB) BeginTx(c context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	tx, err := d.DB.BeginTx(c, opts)
	if err != nil {
		return nil, err
	}
	return &work645AfterHoldTx{Tx: tx, after: d.after}, nil
}
func (tx *work645AfterHoldTx) Begin(c context.Context) (pgx.Tx, error) {
	sp, err := tx.Tx.Begin(c)
	if err != nil {
		return nil, err
	}
	return &work645AfterHoldTx{Tx: sp, after: tx.after, nested: true}, nil
}
func (tx *work645AfterHoldTx) Commit(c context.Context) error {
	if err := tx.Tx.Commit(c); err != nil {
		return err
	}
	if tx.nested && tx.after != nil {
		return tx.after()
	}
	return nil
}
