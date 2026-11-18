package artifact_test

import (
	"context"
	"errors"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/storekit"
)

var ctx = context.Background()

func ok(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// The seed (the actors an artifact's author is one of) and nothing else of
// the store's shared fixture. Importing the store is also what registers
// the check of view artifacts, as in every binary that stores one.
func seedOnly(ctx context.Context, owner *pgx.Conn) error {
	seeded, err := store.Seed(ctx, owner)
	if err == nil && !seeded {
		err = errors.New("seed did nothing on an empty database")
	}
	return err
}

// world is a seeded database with the missions M-1 and M-2, which a search
// by mission names.
type world struct{ rt *pgx.Conn }

func newWorld(t *testing.T) *world {
	t.Helper()
	db := agentrttest.MigratedWith(t, "seeded", seedOnly)
	w := &world{rt: db.Connect(t, db.Runtime)}
	deadline := time.Now().Add(7 * 24 * time.Hour)
	for _, m := range []string{"M-1", "M-2"} {
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			_, err := storekit.CreateMission(ctx, tx, "controller", storekit.Mission{ID: m, WorkID: "W-31", Title: m, Goal: "test", Deadline: deadline})
			return err
		}))
	}
	return w
}

func wr(actor string) store.Writer { return store.Writer{Actor: actor, Token: "operator"} }

func (w *world) artifact(t *testing.T, kind, body string) string {
	t.Helper()
	var id string
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		var err error
		id, err = artifact.PutArtifact(ctx, tx, "controller", kind, "text/plain", []byte(body))
		return err
	}))
	return id
}

// legacy writes rows with the import event, which may write any table.
func (w *world) legacy(t *testing.T, stmts ...string) {
	t.Helper()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test"}}); err != nil {
			return err
		}
		for _, q := range stmts {
			if _, err := tx.Exec(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
}
