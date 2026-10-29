package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// stmtDB counts every statement of every transaction.
type stmtDB struct {
	store.DB
	n int
}

func (d *stmtDB) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	tx, err := d.DB.BeginTx(ctx, o)
	if err != nil {
		return nil, err
	}
	return &stmtTx{Tx: tx, d: d}, nil
}

type stmtTx struct {
	pgx.Tx
	d *stmtDB
}

func (t *stmtTx) Query(ctx context.Context, q string, a ...any) (pgx.Rows, error) {
	t.d.n++
	return t.Tx.Query(ctx, q, a...)
}
func (t *stmtTx) QueryRow(ctx context.Context, q string, a ...any) pgx.Row {
	t.d.n++
	return t.Tx.QueryRow(ctx, q, a...)
}
func (t *stmtTx) Exec(ctx context.Context, q string, a ...any) (pgconn.CommandTag, error) {
	t.d.n++
	return t.Tx.Exec(ctx, q, a...)
}

// ordinaryCIStatements is what one CI step of an ordinary kicked operation
// whose CI runs cost before C-5 (b99f7daf), measured with this very test.
const ordinaryCIStatements = 20

// Serial: FreezeEventClock. R-05: an ordinary operation's CI step (kicked,
// its run still going) costs the round trips it did before C-5: no read of
// a pinned key it cannot have. (On 082125dd: one more transaction.)
func TestC5R05OrdinaryCIStepCostsWhatItDid(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := liveWorld(t, "none", at)
	store.FreezeEventClock(at.Add(-40 * time.Minute))
	root := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "698 R-05", BaseCommit: commitB})
	w.passRound(t, root, "h1", "h2", "h3")
	src := c4Source("console/src/components/C5.tsx")
	store.FreezeEventClock(at.Add(-30 * time.Minute))
	op, d, err := store.ClaimLanding(ctx, w.rt, src, "prod", root, at)
	ok(t, err)
	if op == "" || !d.Allowed {
		t.Fatal("claim", d)
	}
	c4Prepare(t, w, root, op, at.Add(-29*time.Minute), true)
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "ci_kick", "try", 1)))
	store.FreezeEventClock(at.Add(-20 * time.Minute))
	running := landing.CIRun{Run: landing.CIName(op, 1), R: commitB, Status: "Running", CreatedAt: at.Add(-25 * time.Minute)}
	ensured := 0
	srcs := controller.DrillSources{Facts: src,
		Main:          func(context.Context, string) (string, error) { return commitA, nil },
		WindowBlocked: func(context.Context, string) (bool, error) { return false, nil },
		CI:            func(context.Context, string, string, time.Time) (landing.CIRun, error) { return running, nil },
		EnsureCI:      func(context.Context, string, string, string) error { ensured++; return nil }}
	db := &stmtDB{DB: w.rt}
	ctl := controller.Controller{DB: db, Env: "prod", Live: true, DrillGit: map[string]controller.DrillGit{"bifrost-platform": &branchObserved{}}}
	_, err = ctl.LandingDrillCI(ctx, srcs, op, at.Add(-20*time.Minute))
	ok(t, err)
	if ensured != 1 {
		t.Fatal("the CI step did not resume its run", ensured)
	}
	if db.n != ordinaryCIStatements {
		t.Fatalf("the CI step made %d round trips; before C-5 (b99f7daf) it made %d", db.n, ordinaryCIStatements)
	}
}
