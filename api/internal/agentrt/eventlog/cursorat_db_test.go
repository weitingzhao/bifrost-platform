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
// vocabulary is needed. The horizon is the cluster's oldest running
// transaction, which another test's may hold back for a while: the reads
// wait until both rows are below it.
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
		{`INSERT INTO agentrt.event (event_stream_id, seq, event_type, actor_id, body_json, prev_sha256, sha256, at)
		    VALUES ($1, 1, 'note', 'controller', '{"text":"a"}', '', repeat('0', 64), $2)`, []any{stream, a}},
		{`INSERT INTO agentrt.event (event_stream_id, seq, event_type, actor_id, body_json, prev_sha256, sha256, at)
		    VALUES ($1, 2, 'note', 'controller', '{"text":"b"}', repeat('0', 64), repeat('1', 64), $2)`, []any{stream, b}},
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
	deadline := time.Now().Add(15 * time.Second)
	got := read(a.Add(-time.Millisecond))
	for len(got) < 2 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		got = read(a.Add(-time.Millisecond))
	}
	if len(got) != 2 {
		t.Fatalf("from before both: %v", got)
	}
	if got := read(b); len(got) != 1 || got[0] != "b" {
		t.Errorf("from between: %v", got)
	}
	if got := read(b.Add(time.Millisecond)); len(got) != 0 {
		t.Errorf("from after both: %v", got)
	}
}
