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

// database0042 is a database migrated to 0041, its owner's and superuser's
// connections, and the files up to and including 0042.
func database0042(t *testing.T) (*pgx.Conn, *pgx.Conn, []migrate.File) {
	t.Helper()
	db := agentrttest.New(t)
	conn := db.Connect(t, db.Owner)
	files, err := migrate.Files()
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(files, func(f migrate.File) bool { return f.ID == 42 })
	if i < 0 {
		t.Fatal("0042 is missing")
	}
	if _, err = migrate.RunFiles(context.Background(), conn, "dev", files[:i]); err != nil {
		t.Fatal(err)
	}
	return conn, db.Connect(t, db.Admin), files[:i+1]
}

func outboxLeft0042(ctx context.Context, t *testing.T, conn *pgx.Conn) (tables, functions int) {
	t.Helper()
	if err := conn.QueryRow(ctx, `SELECT
	    (SELECT count(*) FROM pg_tables WHERE schemaname = 'agentrt' AND tablename = 'outbox_retired'),
	    (SELECT count(*) FROM pg_proc WHERE pronamespace = 'agentrt'::regnamespace AND proname IN ('guard_outbox', 'notify_outbox'))`).
		Scan(&tables, &functions); err != nil {
		t.Fatal(err)
	}
	return tables, functions
}

// putSignal0042 puts one outbox signal there as the old writer left it, with
// its event, triggers off (no code writes either any more), as 0028's test does.
func putSignal0042(ctx context.Context, t *testing.T, admin *pgx.Conn, createdAt string, delivered bool) {
	t.Helper()
	deliveredAt := "NULL"
	if delivered {
		deliveredAt = "now()"
	}
	if _, err := admin.Exec(ctx, `SET session_replication_role = replica;
	  INSERT INTO agentrt.event_stream (event_stream_id, last_seq, last_sha256) VALUES ('x:1', 1, repeat('0', 64));
	  INSERT INTO agentrt.event (event_stream_id, seq, event_type, actor_id, body_json, prev_sha256, sha256, at)
	    VALUES ('x:1', 1, 'note', 'controller', '{}', '', repeat('0', 64), now());
	  INSERT INTO agentrt.outbox_retired (event_id, topic, payload_json, created_at, delivered_at, tries)
	    SELECT event_id, 'mbox:thread:x', '{}', `+createdAt+`, `+deliveredAt+`, 1 FROM agentrt.event WHERE event_stream_id = 'x:1';
	  SET session_replication_role = origin`); err != nil {
		t.Fatalf("putting the row there: %v", err)
	}
}

// Origin: 2026-11-21, the Owner in the decoupling thread ("c5 现在就删"):
// agentrt.outbox_retired, retired by 0028 with 2,309 delivered rows on PROD and
// no writer since 11-15, is dropped with its two functions.
func TestDrop0042RemovesTheRetiredOutbox(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	conn, admin, files := database0042(t)
	putSignal0042(ctx, t, admin, "timestamptz '2026-11-15 05:33:12+00'", true)
	if tb, fn := outboxLeft0042(ctx, t, conn); tb != 1 || fn != 2 {
		t.Fatalf("before 0042: %d tables, %d functions; want 1 and 2", tb, fn)
	}
	if applied, err := migrate.RunFiles(ctx, conn, "dev", files); err != nil || len(applied) != 1 || applied[0] != 42 {
		t.Fatalf("applied %v: %v", applied, err)
	}
	if tb, fn := outboxLeft0042(ctx, t, conn); tb != 0 || fn != 0 {
		t.Fatalf("after 0042: %d tables, %d functions left; want none", tb, fn)
	}
}

// Origin: same decision. The look-before-acting step is the only thing that
// keeps an unknown writer's rows; these are its positive controls: a row newer
// than the measurement, or an undelivered one, stops the file and nothing is
// dropped.
func TestDrop0042RefusesWhenTheOutboxIsStillWritten(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, createdAt, want string
		delivered             bool
	}{
		{"written after the measurement", "now()", "after it was measured as stopped", true},
		{"undelivered row", "timestamptz '2026-11-15 05:33:12+00'", "undelivered rows", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			conn, admin, files := database0042(t)
			putSignal0042(ctx, t, admin, c.createdAt, c.delivered)
			_, err := migrate.RunFiles(ctx, conn, "dev", files)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("0042 ran: %v; want a refusal naming %q", err, c.want)
			}
			if tb, fn := outboxLeft0042(ctx, t, conn); tb != 1 || fn != 2 {
				t.Fatalf("after the refusal: %d tables, %d functions; want 1 and 2", tb, fn)
			}
		})
	}
}
