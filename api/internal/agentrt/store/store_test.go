package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The fixtures the rule tests stand on. The cross-domain tests left for
// agentrt/integration on 2026-11-18 and keep their own copy (world_test.go), so
// that this package does not depend on that one.

var ctx = context.Background()

func ctxOf(t testing.TB) context.Context { return store.CtxOf(t) }

func ok(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// world is a migrated, seeded database.
type world struct {
	db *agentrttest.DB
	rt *pgx.Conn
	// t is the test the world was made for and ctx its context (ctxOf), for
	// a helper that has the world and no t.
	t   *testing.T
	ctx context.Context
}

// seededDB is a migrated database the seed has run on: a copy of a template
// seeded once per test process (CI-14), not a seed per test.
func seededDB(t *testing.T) *agentrttest.DB {
	t.Helper()
	return agentrttest.MigratedWith(t, seededTemplate, seedTemplate)
}

// seededTemplate names the template seedTemplate prepares. TestMain makes it
// before any test runs, so the seed's events carry the wall clock whatever
// event clock the first test to want a world has frozen.
const seededTemplate = "seeded"

func seedTemplate(ctx context.Context, owner *pgx.Conn) error {
	seeded, err := actor.Seed(ctx, owner)
	if err == nil && !seeded {
		err = errors.New("seed did nothing on an empty database")
	}
	return err
}

func newWorld(t *testing.T) *world {
	t.Helper()
	db := seededDB(t)
	return &world{db: db, rt: db.Connect(t, db.Runtime), t: t, ctx: ctxOf(t)}
}

// newKernelWorld is a world with hosts h1–h8.
func newKernelWorld(t *testing.T) *world {
	t.Helper()
	ctx := ctxOf(t)
	w := newWorld(t)
	w.seeded(t, func(tx pgx.Tx) {
		for i := 1; i <= 8; i++ {
			seedHost(t, tx, fmt.Sprintf("h%d", i))
		}
	})
	_, err := w.rt.Exec(ctx, agentrttest.DispatchRules)
	ok(t, err)
	return w
}

// Scenes of the retired dispatch lane, written as its rows (store cut 3b-2,
// 2026-11-14). Nothing leases, runs or checks a task any more, but the plan's
// bindings, the artifact search and the progress read still read the rows a
// round left: the tasks, their attempts and the verifications. These
// fixtures write those rows as an import does (a legacy_imported event of
// the transaction), in the shape the lane left them.

// seedHost makes host id exist: an attempt names the host it ran on.
func seedHost(t *testing.T, tx pgx.Tx, id string) {
	t.Helper()
	_, err := tx.Exec(ctx, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name)
 VALUES ('host:' || $1, 'host', 'Agent host ' || $1) ON CONFLICT DO NOTHING`, id)
	ok(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO agentrt.host (host_id, actor_id, host_kind, state)
 VALUES ($1, 'host:' || $1, 'dev', 'active') ON CONFLICT DO NOTHING`, id)
	ok(t, err)
}

// seeded runs fn in a transaction that carries an import event, as a
// fixture's rows need.
func (w *world) seeded(t *testing.T, fn func(tx pgx.Tx)) {
	t.Helper()
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test"}}); err != nil {
			return err
		}
		fn(tx)
		return nil
	}))
}
