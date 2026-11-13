package migrate_test

import (
	"context"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// 0017 adds the evidence ledger and touches nothing that was there: events
// are as they were, the table takes a row once per run and result, and a row
// is never changed or removed.
func TestUpgrade0017AddsAppendOnlyEvidence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := agentrttest.New(t)
	conn := db.Connect(t, db.Owner)
	files, err := migrate.Files()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = migrate.RunFiles(ctx, conn, "dev", files[:16]); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Seed(ctx, conn); err != nil {
		t.Fatal(err)
	}
	const query = `SELECT COALESCE(string_agg(to_jsonb(e)::text,E'\n' ORDER BY event_id),'') FROM agentrt.event e`
	var before, after string
	if err = conn.QueryRow(ctx, query).Scan(&before); err != nil {
		t.Fatal(err)
	}
	applied, err := migrate.RunFiles(ctx, conn, "dev", files[:17])
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 || applied[0] != 17 {
		t.Fatalf("applied %v", applied)
	}
	if err = conn.QueryRow(ctx, query).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before == "" || before != after {
		t.Fatal("upgrade changed events, or the fixture has none")
	}
	for _, name := range []string{"evidence", "evidence_key_idx", "evidence_commit_idx"} {
		var exists bool
		if err = conn.QueryRow(ctx, `SELECT to_regclass('agentrt.' || $1) IS NOT NULL`, name).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Errorf("missing %s", name)
		}
	}
	const insert = `INSERT INTO agentrt.evidence (repo, unit, task, input_key, result, scope, inputs, statement, commit_sha, run_name, run_uid)
	  VALUES ('bifrost-platform','repo','bifrost-ci-platform',$1,$2,'full','{}','{}',$3,'ci-platform-1','uid-1') ON CONFLICT (run_uid, result) DO NOTHING`
	key, commit := strings.Repeat("a", 64), strings.Repeat("b", 40)
	for i, want := range []int64{1, 0} {
		tag, err := conn.Exec(ctx, insert, key, "passed", commit)
		if err != nil || tag.RowsAffected() != want {
			t.Fatalf("insert %d: %d rows, %v", i, tag.RowsAffected(), err)
		}
	}
	if _, err = conn.Exec(ctx, insert, key, "green", commit); err == nil {
		t.Fatal("a result outside the three was taken")
	}
	for _, change := range []string{`UPDATE agentrt.evidence SET result='failed'`, `DELETE FROM agentrt.evidence`, `TRUNCATE agentrt.evidence`} {
		if _, err = conn.Exec(ctx, change); err == nil {
			t.Fatalf("%s was allowed", change)
		}
	}
	// The runtime role may read and append; the reader may read.
	for role, want := range map[string]string{"platform_agentrt_dev": "SELECT,INSERT", "agent_reader": "SELECT"} {
		var got string
		if err = conn.QueryRow(ctx, `SELECT COALESCE(string_agg(p, ',' ORDER BY p DESC),'') FROM unnest(ARRAY['SELECT','INSERT','UPDATE','DELETE','TRUNCATE']) p
		  WHERE has_table_privilege($1, 'agentrt.evidence', p)`, role).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s may %s, want %s", role, got, want)
		}
	}
}

// 0018: the same run under another key is another row; the same row again is
// not; rows written before it are kept.
func TestUpgrade0018ARunMayHaveARowPerKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := agentrttest.New(t)
	conn := db.Connect(t, db.Owner)
	files, err := migrate.Files()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = migrate.RunFiles(ctx, conn, "dev", files[:17]); err != nil {
		t.Fatal(err)
	}
	const insert = `INSERT INTO agentrt.evidence (repo, unit, task, input_key, result, scope, inputs, statement, commit_sha, run_name, run_uid)
	  VALUES ('bifrost-platform','repo','bifrost-ci-platform',$1,'passed','full','{}','{}',$2,'ci-platform-1','uid-1')`
	wrong, right, commit := strings.Repeat("a", 64), strings.Repeat("c", 64), strings.Repeat("b", 40)
	if _, err = conn.Exec(ctx, insert, wrong, commit); err != nil {
		t.Fatal(err)
	}
	if applied, err := migrate.RunFiles(ctx, conn, "dev", files[:18]); err != nil || len(applied) != 1 || applied[0] != 18 {
		t.Fatalf("applied %v: %v", applied, err)
	}
	const onConflict = ` ON CONFLICT (run_uid, repo, unit, task, input_key, result) DO NOTHING`
	for i, tc := range []struct {
		key  string
		want int64
	}{{right, 1}, {right, 0}, {wrong, 0}} {
		tag, err := conn.Exec(ctx, insert+onConflict, tc.key, commit)
		if err != nil || tag.RowsAffected() != tc.want {
			t.Fatalf("insert %d: %d rows, %v", i, tag.RowsAffected(), err)
		}
	}
	var n int
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM agentrt.evidence`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("%d rows, %v", n, err)
	}
}
