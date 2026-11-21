package eventlog_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Origin: 2026-11-21, the cluster gate takes back the objects a full list no
// longer holds (392 workloads in the state readout for 82 in the cluster);
// it reads each object's stream's latest event. LatestOfStreams gives one
// row per stream named prefix+..., the latest of the type, since a time:
// not another prefix's streams, not another type, not an older event.
func TestLatestOfStreamsIsOneRowPerStream(t *testing.T) {
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	admin := db.Connect(t, db.Admin)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := admin.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("putting the rows there: %v", err)
		}
	}
	exec(`SET session_replication_role = replica`)
	put := func(stream string, seq int, typ, text string, at time.Time) {
		exec(`INSERT INTO agentrt.event_stream (event_stream_id, last_seq, last_sha256) VALUES ($1, 0, repeat('0', 64)) ON CONFLICT DO NOTHING`, stream)
		exec(`INSERT INTO agentrt.event (event_stream_id, seq, event_type, actor_id, body_json, prev_sha256, sha256, at, txid)
		    VALUES ($1, $2, $3, 'controller', jsonb_build_object('text', $4::text), CASE WHEN $2::bigint = 1 THEN '' ELSE repeat('0', 64) END, repeat('1', 64), $5, 1)`, stream, seq, typ, text, at)
	}
	put("sts:cicd/a", 1, "note", "a1", now.Add(-3*time.Minute))
	put("sts:cicd/a", 2, "note", "a2", now.Add(-2*time.Minute))
	put("sts:cicd/b", 1, "note", "b1", now.Add(-time.Minute))
	put("sts:cicd/b", 2, "actor_registered", "b2", now)
	put("deploy:cicd/a", 1, "note", "d1", now)
	put("sts:cicd/old", 1, "note", "old", now.Add(-48*time.Hour))
	pool := db.Pool(t, db.Runtime)
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := eventlog.LatestOfStreams(ctx, tx, "note", "sts:", now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range rows {
		got = append(got, r.Stream+"="+r.Body["text"].(string))
	}
	sort.Strings(got)
	if len(got) != 2 || got[0] != "sts:cicd/a=a2" || got[1] != "sts:cicd/b=b1" {
		t.Fatalf("got %v, want [sts:cicd/a=a2 sts:cicd/b=b1]", got)
	}
}
