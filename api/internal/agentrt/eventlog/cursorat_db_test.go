package eventlog_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Origin: 2026-11-17, the Bus page's replay of the last day (bus gate
// since=). Reading on from CursorAt(t) lists the events written at or after
// t and not one before; from a time before both, both; from a time after
// every event, none (the horizon). The two rows are put there by the
// superuser with triggers off, with their times set, so no domain's
// vocabulary is needed.
//
// The rows carry transaction ids 1 and 2, below any horizon. 2026-11-18 (TD-583):
// they used to carry the inserting transaction's id, and the horizon is the
// oldest running transaction of the whole cluster, which every test shares;
// a long transaction in another package held the rows above it past the
// 15-second wait and the test read "from before both: []" (comm line, a full
// local run of ./internal/agentrt/...). A database of its own would not help:
// the horizon is not per database.
func TestCursorAtReadsFromATime(t *testing.T) {
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	const stream = "contract:cursor-at"
	base := time.Now().Add(-time.Hour).UTC().Truncate(time.Millisecond)
	a, b := base, base.Add(time.Second)
	admin := db.Connect(t, db.Admin)
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`SET session_replication_role = replica`, nil},
		{`INSERT INTO agentrt.event_stream (event_stream_id, last_seq, last_sha256) VALUES ($1, 2, repeat('0', 64))`, []any{stream}},
		// Two transactions, as two writers would: one transaction's events share its txid.
		{`INSERT INTO agentrt.event (event_stream_id, seq, event_type, actor_id, body_json, prev_sha256, sha256, at, txid)
		    VALUES ($1, 1, 'note', 'controller', '{"text":"a"}', '', repeat('0', 64), $2, 1)`, []any{stream, a}},
		{`INSERT INTO agentrt.event (event_stream_id, seq, event_type, actor_id, body_json, prev_sha256, sha256, at, txid)
		    VALUES ($1, 2, 'note', 'controller', '{"text":"b"}', repeat('0', 64), repeat('1', 64), $2, 2)`, []any{stream, b}},
	} {
		if _, err := admin.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("putting the rows there: %v", err)
		}
	}
	pool := db.Pool(t, db.Runtime)
	read := func(at time.Time) []string {
		var out []string
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		c, err := eventlog.CursorAt(ctx, tx, at)
		if err != nil {
			t.Fatal(err)
		}
		doc, _, err := eventlog.EventsAfter(ctx, tx, c, 100, eventlog.EventFilter{Types: []string{"note"}})
		if err != nil {
			t.Fatal(err)
		}
		var rows []struct {
			Stream string         `json:"event_stream_id"`
			Body   map[string]any `json:"body_json"`
		}
		if err := json.Unmarshal(doc, &rows); err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			if r.Stream == stream {
				out = append(out, r.Body["text"].(string))
			}
		}
		return out
	}
	if got := read(a.Add(-time.Millisecond)); len(got) != 2 {
		t.Fatalf("from before both: %v", got)
	}
	if got := read(b); len(got) != 1 || got[0] != "b" {
		t.Errorf("from between: %v", got)
	}
	if got := read(b.Add(time.Millisecond)); len(got) != 0 {
		t.Errorf("from after both: %v", got)
	}
}

// Positive control for TestCursorAtReadsFromATime (TD-583, 2026-11-21): a row
// written with its own transaction's id stays unread while an older
// transaction anywhere in the cluster is open. This is the cause the old test
// ran into, caught on purpose. It does not wait for the row to appear after
// the older transaction ends: that wait depends on every other test's
// transactions too (it failed so in a full -count=10 run), which is the
// flakiness this fix removes.
func TestCursorAtWaitsForTheOldestOpenTransaction(t *testing.T) {
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	const stream = "contract:cursor-at-held"
	at := time.Now().Add(-time.Hour).UTC().Truncate(time.Millisecond)
	held := db.Connect(t, db.Admin)
	if _, err := held.Exec(ctx, `BEGIN`); err != nil {
		t.Fatal(err)
	}
	var heldID int64
	if err := held.QueryRow(ctx, `SELECT txid_current()`).Scan(&heldID); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = held.Exec(ctx, `ROLLBACK`) }()
	admin := db.Connect(t, db.Admin)
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`SET session_replication_role = replica`, nil},
		{`INSERT INTO agentrt.event_stream (event_stream_id, last_seq, last_sha256) VALUES ($1, 1, repeat('0', 64))`, []any{stream}},
		{`INSERT INTO agentrt.event (event_stream_id, seq, event_type, actor_id, body_json, prev_sha256, sha256, at)
		    VALUES ($1, 1, 'note', 'controller', '{"text":"a"}', '', repeat('0', 64), $2)`, []any{stream, at}},
	} {
		if _, err := admin.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("putting the row there: %v", err)
		}
	}
	pool := db.Pool(t, db.Runtime)
	count := func() int {
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		c, err := eventlog.CursorAt(ctx, tx, at.Add(-time.Millisecond))
		if err != nil {
			t.Fatal(err)
		}
		doc, _, err := eventlog.EventsAfter(ctx, tx, c, 100, eventlog.EventFilter{Types: []string{"note"}})
		if err != nil {
			t.Fatal(err)
		}
		var rows []struct {
			Stream string `json:"event_stream_id"`
		}
		if err := json.Unmarshal(doc, &rows); err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, r := range rows {
			if r.Stream == stream {
				n++
			}
		}
		return n
	}
	var rowID, horizon int64
	if err := admin.QueryRow(ctx, `SELECT txid FROM agentrt.event WHERE event_stream_id = $1`, stream).Scan(&rowID); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `SELECT txid_snapshot_xmin(txid_current_snapshot())`).Scan(&horizon); err != nil {
		t.Fatal(err)
	}
	if horizon > heldID || rowID <= heldID {
		t.Fatalf("horizon %d, held transaction %d, row %d: the row should be above an open older transaction", horizon, heldID, rowID)
	}
	if n := count(); n != 0 {
		t.Fatalf("read %d rows above an open older transaction, want 0", n)
	}
}
