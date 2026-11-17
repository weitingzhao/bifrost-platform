package migrate_test

import (
	"context"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
)

// Origin: 2026-11-17, the communication line retiring agentrt.outbox (its
// writer left on 11-15, 7fc1eff2; PROD held 2,309 delivered rows). 0028 only
// renames: every row is there under the new name, the old name is gone, and
// the guard on the rows still holds.
func TestUpgrade0028RetiresTheOutboxKeepingItsRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := agentrttest.New(t)
	conn := db.Connect(t, db.Owner)
	files, err := migrate.Files()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 28 || files[27].ID != 28 {
		t.Fatal("0028 is missing")
	}
	if _, err = migrate.RunFiles(ctx, conn, "dev", files[:27]); err != nil {
		t.Fatal(err)
	}
	// A delivered signal as the old writer left it, with its event. No code
	// writes either any more, so the superuser puts them there with triggers
	// off, as 0023's test does.
	admin := db.Connect(t, db.Admin)
	if _, err = admin.Exec(ctx, `SET session_replication_role = replica;
	  INSERT INTO agentrt.event_stream (event_stream_id, last_seq, last_sha256) VALUES ('x:1', 1, repeat('0', 64));
	  INSERT INTO agentrt.event (event_stream_id, seq, event_type, actor_id, body_json, prev_sha256, sha256, at)
	    VALUES ('x:1', 1, 'note', 'controller', '{}', '', repeat('0', 64), now());
	  INSERT INTO agentrt.outbox (event_id, topic, payload_json, delivered_at, tries)
	    SELECT event_id, 'mbox:thread:x', '{}', now(), 1 FROM agentrt.event WHERE event_stream_id = 'x:1'`); err != nil {
		t.Fatalf("putting the row there: %v", err)
	}
	if applied, err := migrate.RunFiles(ctx, conn, "dev", files[:28]); err != nil || len(applied) != 1 || applied[0] != 28 {
		t.Fatalf("applied %v: %v", applied, err)
	}
	var rows int
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM agentrt.outbox_retired WHERE topic = 'mbox:thread:x' AND delivered_at IS NOT NULL`).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("retired rows %d: %v", rows, err)
	}
	var old *string
	if err = conn.QueryRow(ctx, `SELECT to_regclass('agentrt.outbox')::text`).Scan(&old); err != nil || old != nil {
		t.Fatalf("agentrt.outbox still there: %v %v", old, err)
	}
	if _, err = conn.Exec(ctx, `UPDATE agentrt.outbox_retired SET topic = 'wake'`); err == nil {
		t.Fatal("the guard no longer holds on the retired rows")
	}
}
