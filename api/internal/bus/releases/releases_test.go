package releases_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor" // the seed, in the test only
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/releases"
)

var ctx = context.Background()

// 2026-11-16, the CI line's release record draft: the record and its
// announcement commit together; the event id goes into the record.
func TestAppendInTheRecordsTransaction(t *testing.T) {
	db := agentrttest.Migrated(t)
	if _, err := actor.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pool := db.Pool(t, db.Runtime)
	r := releases.Release{Env: "stg", Lane: "platform", Repo: "bifrost-platform", SHA: strings.Repeat("a", 40), Run: "bifrost-deliver-platform-1794883407", Succeeded: true}
	var id int64
	if err := eventlog.InTx(ctx, pool, func(tx pgx.Tx) error {
		var err error
		id, err = releases.Append(ctx, tx, r)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var subject, change string
	if err := pool.QueryRow(ctx, `SELECT body_json->>'subject', body_json->>'change' FROM agentrt.event WHERE event_id = $1`, id).Scan(&subject, &change); err != nil {
		t.Fatal(err)
	}
	if id == 0 || subject != "stg/platform@"+r.SHA || change != releases.ChangeSucceeded {
		t.Fatalf("id=%d subject=%q change=%q", id, subject, change)
	}
	// Rolled back with the record: nothing is announced.
	failed := r
	failed.Succeeded = false
	_ = eventlog.InTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := releases.Append(ctx, tx, failed); err != nil {
			return err
		}
		return context.Canceled // the record's insert failed
	})
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = $1`, releases.Type).Scan(&n); err != nil || n != 1 {
		t.Fatalf("events=%d err=%v: a rolled-back record announced", n, err)
	}
}

func TestReleasesAgreeWithTheRegister(t *testing.T) {
	if err := registry.Agree(releases.Type); err != nil {
		t.Error(err)
	}
}
