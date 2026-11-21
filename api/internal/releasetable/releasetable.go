// Package releasetable is the release records' table as the recorder writes
// and reads it: each record a row, and its announcement on the bus in the same
// transaction.
package releasetable

import (
	"context"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
)

// ledgerCause is the ledger event a run's cause label names ("ev-<n>"), when
// this ledger has it; 0 otherwise. Head-release and the recorder both run in
// PROD's workers, so the label names an event of this ledger; one it does not
// have is left out rather than failing the row on the foreign key.
func ledgerCause(ctx context.Context, tx pgx.Tx, label string) (int64, error) {
	n, ok := strings.CutPrefix(label, "ev-")
	if !ok {
		return 0, nil
	}
	id, err := strconv.ParseInt(n, 10, 64)
	if err != nil || id <= 0 {
		return 0, nil
	}
	found, err := eventlog.Exists(ctx, tx, id)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, nil
	}
	return id, nil
}

// Rows reads the record table and copies into it the records it lacks.
type Rows struct{ Pool *pgxpool.Pool }

func (r Rows) Records(ctx context.Context) ([]releases.Record, error) {
	return releases.ReadRecords(ctx, r.Pool)
}

func (r Rows) Deployments(ctx context.Context, lane, env string) ([]releases.Record, error) {
	return releases.ReadDeployments(ctx, r.Pool, lane, env)
}

func (r Rows) MissingByRun(ctx context.Context, runs []string) (map[string]int, error) {
	return releases.ReadMissingByRun(ctx, r.Pool, runs)
}

func (r Rows) Backfill(ctx context.Context, rec releases.Record) (bool, error) {
	var wrote bool
	err := eventlog.InTx(ctx, r.Pool, func(tx pgx.Tx) error {
		var err error
		wrote, err = releases.BackfillRecord(ctx, tx, rec)
		return err
	})
	return wrote, err
}

// Writer is the recorder's table writer once the ledger's pool is open
// (2026-11-17, step 1 of moving the release records into the database): the
// announcement release.deploy.finished first, then the row naming it, in one
// transaction (the bus RULES §7: state and event together). A record whose
// revision names no single repository's built commit is a row without an
// announcement. A run with a row already (a record built again) is a new
// row naming the first announcement, and is not announced again.
func Writer(pool *pgxpool.Pool) releases.TableWriter {
	return func(ctx context.Context, rec releases.Record, succeeded bool) error {
		return eventlog.InTx(ctx, pool, func(tx pgx.Tx) error {
			eventID, again, err := releases.PriorEvent(ctx, tx, rec.Run)
			if err != nil {
				return err
			}
			if repo, sha, ok := releases.Announced(rec); ok && !again {
				cause, err := ledgerCause(ctx, tx, rec.Cause)
				if err != nil {
					return err
				}
				id, err := appendAnnouncement(ctx, tx, announcement{
					Env: rec.Env, Lane: rec.Lane, Repo: repo, SHA: sha, Run: rec.Run, Succeeded: succeeded, Cause: cause,
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
