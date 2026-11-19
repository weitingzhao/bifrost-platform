package coord_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/storekit"
)

// The fixture of the coordination tests that moved here from package store
// (TD-582): a migrated database with the seed's actors and, for missions M-1 and M-2 (M-1 active in a kernel
// world). It is store's fixture without store and without
// the plan, hosts and jobs no coordination test reads.

const coordinatorSeat = "seat:coordinator"

// wr is a writer as the routes make one: the actor and the token.
func wr(id string) actor.Writer { return actor.Writer{Actor: id, Token: "operator"} }

type world struct {
	db *agentrttest.DB
	rt *pgx.Conn
}

func newWorld(t *testing.T) *world {
	t.Helper()
	db := agentrttest.MigratedWith(t, "seeded", seedOnly)
	w := &world{db: db, rt: db.Connect(t, db.Runtime)}
	deadline := time.Now().Add(7 * 24 * time.Hour)
	for _, m := range []string{"M-1", "M-2"} {
		ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			_, err := storekit.CreateMission(ctx, tx, "controller", storekit.Mission{ID: m, WorkID: "W-31", Title: m, Goal: "test", Deadline: deadline})
			return err
		}))
	}
	return w
}

// newKernelWorld is a world with mission M-1 active.
func newKernelWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error { return storekit.MoveMission(ctx, tx, "controller", "M-1", "active", "test") }))
	return w
}

func (w *world) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	ok(t, w.rt.QueryRow(context.Background(), q, args...).Scan(&n))
	return n
}

func (w *world) chainHolds(t *testing.T, stream string) {
	t.Helper()
	if _, err := eventlog.VerifyStream(ctx, w.rt, stream); err != nil {
		t.Fatalf("%s: %v", stream, err)
	}
}

func (w *world) control(t *testing.T, scope, ref, kind string) string {
	t.Helper()
	got, err := coord.RequestControl(ctx, w.rt, wr("owner"), "k-"+eventlog.NewULID(time.Now()), scope, ref, kind, "test")
	ok(t, err)
	return got["control_id"].(string)
}
