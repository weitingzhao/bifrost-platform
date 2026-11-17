package server

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	busreleases "github.com/weitingzhao/bifrost-platform/api/internal/bus/releases"
	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
)

// releaseTable is the recorder's table writer once the ledger's pool is open
// (2026-11-17, step 1 of moving the release records into the database): the
// announcement release.deploy.finished first, then the row naming it, in one
// transaction (the bus RULES §7: state and event together). A record whose
// revision names no single repository's built commit is a row without an
// announcement. A run with a row already (a record built again) is a new
// row naming the first announcement, and is not announced again.
// releaseRows reads the record table for the recorder's side-by-side
// comparison and copies into it the records written before it (step 2).
type releaseRows struct{ pool *pgxpool.Pool }

func (r releaseRows) Records(ctx context.Context) ([]releases.Record, error) {
	return releases.ReadRecords(ctx, r.pool)
}

func (r releaseRows) Backfill(ctx context.Context, rec releases.Record) (bool, error) {
	var wrote bool
	err := store.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		var err error
		wrote, err = releases.BackfillRecord(ctx, tx, rec)
		return err
	})
	return wrote, err
}

func releaseTable(pool *pgxpool.Pool) releases.TableWriter {
	return func(ctx context.Context, rec releases.Record, succeeded bool) error {
		return store.InTx(ctx, pool, func(tx pgx.Tx) error {
			eventID, again, err := releases.PriorEvent(ctx, tx, rec.Run)
			if err != nil {
				return err
			}
			if repo, sha, ok := releases.Announced(rec); ok && !again {
				id, err := busreleases.Append(ctx, tx, busreleases.Release{
					Env: rec.Env, Lane: rec.Lane, Repo: repo, SHA: sha, Run: rec.Run, Succeeded: succeeded,
				})
				if err != nil {
					return err
				}
				eventID = id
			}
			return releases.InsertRecord(ctx, tx, rec, succeeded, eventID)
		})
	}
}
