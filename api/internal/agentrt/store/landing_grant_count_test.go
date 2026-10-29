package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// statements counts the round trips a transaction makes.
type statements struct {
	pgx.Tx
	n int
}

func (s *statements) Query(ctx context.Context, q string, args ...any) (pgx.Rows, error) {
	s.n++
	return s.Tx.Query(ctx, q, args...)
}
func (s *statements) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	s.n++
	return s.Tx.QueryRow(ctx, q, args...)
}
func (s *statements) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	s.n++
	return s.Tx.Exec(ctx, q, args...)
}

// landingFactsStatements is what the gates' common reader costs on an
// ordinary prepared operation before C-5 (b99f7daf, the integration
// branch just before it): measured there with this very test.
const landingFactsStatements = 7

// Serial: FreezeEventClock. Review 694 R-03/R-04: with no grant anywhere
// and grants not enabled, the gates' common reader makes exactly the
// round trips it made before C-5 — the enabled check rides on the task
// read, and the stream is read once (on dff96c3c: two more, a second read of
// the stream for grants on each of the two stream folds).
func TestC5R04LandingFactsCostsWhatItDid(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := liveWorld(t, "none", at)
	store.FreezeEventClock(at.Add(-40 * time.Minute))
	root := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "694 R-04", BaseCommit: commitB})
	w.passRound(t, root, "h1", "h2", "h3")
	src := c4Source("console/src/components/C5.tsx")
	store.FreezeEventClock(at.Add(-30 * time.Minute))
	op, d, err := store.ClaimLanding(ctx, w.rt, src, "prod", root, at)
	ok(t, err)
	if op == "" || !d.Allowed {
		t.Fatal("claim", d)
	}
	var n int
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		s := &statements{Tx: tx}
		_, err := store.LandingFacts(ctx, s, src, "prod", "live", root, op, "", at)
		n = s.n
		return err
	}))
	if n != landingFactsStatements {
		t.Fatalf("LandingFacts made %d round trips; before C-5 (b99f7daf) it made %d", n, landingFactsStatements)
	}
}
