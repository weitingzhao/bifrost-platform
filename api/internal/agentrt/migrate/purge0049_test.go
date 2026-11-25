package migrate_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
)

const secretStream0049 = "owner-said:7604bfcc-2df4-49aa-877d-8e482c27ed93"

// database0049 is a database migrated to 0048, its owner's and superuser's
// connections, and the files up to and including 0049.
func database0049(t *testing.T) (*pgx.Conn, *pgx.Conn, []migrate.File) {
	t.Helper()
	db := agentrttest.New(t)
	conn := db.Connect(t, db.Owner)
	files, err := migrate.Files()
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(files, func(f migrate.File) bool { return f.ID == 49 })
	if i < 0 {
		t.Fatal("0049 is missing")
	}
	if _, err = migrate.RunFiles(context.Background(), conn, "dev", files[:i]); err != nil {
		t.Fatal(err)
	}
	return conn, db.Connect(t, db.Admin), files[:i+1]
}

// putStream0049 puts a stream of records of the given types there, triggers off
// (as 0042's test does).
func putStream0049(ctx context.Context, t *testing.T, admin *pgx.Conn, stream string, types ...string) {
	t.Helper()
	if _, err := admin.Exec(ctx, "SET session_replication_role = replica"); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "SET session_replication_role = origin") //nolint:errcheck
	if _, err := admin.Exec(ctx, `INSERT INTO agentrt.event_stream (event_stream_id, last_seq, last_sha256) VALUES ($1, $2, repeat('0', 64))`,
		stream, len(types)); err != nil {
		t.Fatalf("putting the stream there: %v", err)
	}
	for i, typ := range types {
		if _, err := admin.Exec(ctx, `INSERT INTO agentrt.event (event_stream_id, seq, event_type, actor_id, body_json, prev_sha256, sha256, at)
		    VALUES ($1, $2::int, $3, 'controller', '{}', CASE WHEN $2::int = 1 THEN '' ELSE repeat('0', 64) END, repeat('0', 64), now())`, stream, i+1, typ); err != nil {
			t.Fatalf("putting record %d there: %v", i+1, err)
		}
	}
}

func count0049(ctx context.Context, t *testing.T, conn *pgx.Conn, stream string) (events, streams int) {
	t.Helper()
	if err := conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM agentrt.event WHERE event_stream_id = $1),
	    (SELECT count(*) FROM agentrt.event_stream WHERE event_stream_id = $1)`, stream).Scan(&events, &streams); err != nil {
		t.Fatal(err)
	}
	return events, streams
}

// Origin: 2026-11-25, the legislation thread. The first backfill of the Owner's
// words kept a database password in three records of one transcript; the
// Owner: 「好，删」. 0049 deletes that one stream and nothing else, and the table
// is append only again after it.
func TestPurge0049DeletesTheOneStreamOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	conn, admin, files := database0049(t)
	putStream0049(ctx, t, admin, secretStream0049, "owner_said", "owner_said", "owner_said")
	other := "owner-said:00000000-0000-0000-0000-000000000001"
	putStream0049(ctx, t, admin, other, "owner_said")
	if applied, err := migrate.RunFiles(ctx, conn, "dev", files); err != nil || len(applied) != 1 || applied[0] != 49 {
		t.Fatalf("applied %v: %v", applied, err)
	}
	if e, s := count0049(ctx, t, conn, secretStream0049); e != 0 || s != 0 {
		t.Fatalf("after 0049 the stream has %d records, %d stream rows; want none", e, s)
	}
	if e, s := count0049(ctx, t, conn, other); e != 1 || s != 1 {
		t.Fatalf("after 0049 another transcript has %d records, %d stream rows; want 1 and 1", e, s)
	}
	// positive control: the triggers are back on, so a delete is refused again
	if _, err := admin.Exec(ctx, `DELETE FROM agentrt.event WHERE event_stream_id = $1`, other); err == nil || !strings.Contains(err.Error(), "append only") {
		t.Fatalf("delete after 0049: %v; want the append-only refusal", err)
	}
}

// Origin: same decision. The stream holds only owner_said records; anything else
// in it stops the file and nothing is deleted.
func TestPurge0049RefusesAStreamWithOtherRecords(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	conn, admin, files := database0049(t)
	putStream0049(ctx, t, admin, secretStream0049, "owner_said", "note")
	if _, err := migrate.RunFiles(ctx, conn, "dev", files); err == nil || !strings.Contains(err.Error(), "not owner_said") {
		t.Fatalf("0049 ran: %v; want a refusal", err)
	}
	if e, s := count0049(ctx, t, conn, secretStream0049); e != 2 || s != 1 {
		t.Fatalf("after the refusal: %d records, %d stream rows; want 2 and 1", e, s)
	}
}
