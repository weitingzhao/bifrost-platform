package migrate_test

import (
	"context"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
)

// 0019 adds the unit runs and touches nothing that was there: evidence rows
// are as they were; a TaskRun gives one row; a run the content was not judged
// in (infra) needs none of the columns a result fills; a row is never changed
// or removed.
func TestUpgrade0019AddsAppendOnlyUnitRuns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := agentrttest.New(t)
	conn := db.Connect(t, db.Owner)
	files, err := migrate.Files()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = migrate.RunFiles(ctx, conn, "dev", files[:18]); err != nil {
		t.Fatal(err)
	}
	key, commit := strings.Repeat("a", 64), strings.Repeat("b", 40)
	if _, err = conn.Exec(ctx, `INSERT INTO agentrt.evidence (repo, unit, task, input_key, result, scope, inputs, statement, commit_sha, run_name, run_uid)
	  VALUES ('bifrost-platform','repo','bifrost-ci-platform',$1,'passed','full','{}','{}',$2,'ci-platform-1','uid-1')`, key, commit); err != nil {
		t.Fatal(err)
	}
	const query = `SELECT COALESCE(string_agg(to_jsonb(e)::text,E'\n' ORDER BY evidence_id),'') FROM agentrt.evidence e`
	var before, after string
	if err = conn.QueryRow(ctx, query).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if applied, err := migrate.RunFiles(ctx, conn, "dev", files[:19]); err != nil || len(applied) != 1 || applied[0] != 19 {
		t.Fatalf("applied %v: %v", applied, err)
	}
	if err = conn.QueryRow(ctx, query).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before == "" || before != after {
		t.Fatal("upgrade changed evidence, or the fixture has none")
	}
	for _, name := range []string{"unit_run", "unit_run_key_idx", "unit_run_plan_idx"} {
		var exists bool
		if err = conn.QueryRow(ctx, `SELECT to_regclass('agentrt.' || $1) IS NOT NULL`, name).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Errorf("missing %s", name)
		}
	}
	// An infra row: no result of the run was read.
	const infra = `INSERT INTO agentrt.unit_run (plan, repo, unit, task, input_key, commit_sha, shard_index, shard_total, attempt, class, reason, run_name, run_uid)
	  VALUES ($1,'bifrost-platform','api/internal/unitplan','test',$2,$3,$4,$5,1,$6,'TaskRunImagePullFailed','unit-0123abcd-1',$7) ON CONFLICT (run_uid) DO NOTHING`
	plan := strings.Repeat("c", 16)
	for i, want := range []int64{1, 0} {
		tag, err := conn.Exec(ctx, infra, plan, key, commit, 0, 1, "infra", "uid-a")
		if err != nil || tag.RowsAffected() != want {
			t.Fatalf("insert %d: %d rows, %v", i, tag.RowsAffected(), err)
		}
	}
	for name, args := range map[string][]any{
		"a class outside the four":    {plan, key, commit, 0, 1, "green", "uid-b"},
		"a part beyond the total":     {plan, key, commit, 8, 8, "passed", "uid-c"},
		"a plan that is not a hash":   {"plan-1", key, commit, 0, 1, "passed", "uid-d"},
		"a key that is not a sha256":  {plan, "abc", commit, 0, 1, "passed", "uid-e"},
		"a commit that is not a sha1": {plan, key, "main", 0, 1, "passed", "uid-f"},
	} {
		if _, err = conn.Exec(ctx, infra, args...); err == nil {
			t.Errorf("%s was taken", name)
		}
	}
	for _, change := range []string{`UPDATE agentrt.unit_run SET class='passed'`, `DELETE FROM agentrt.unit_run`, `TRUNCATE agentrt.unit_run`} {
		if _, err = conn.Exec(ctx, change); err == nil {
			t.Fatalf("%s was allowed", change)
		}
	}
	for role, want := range map[string]string{"platform_agentrt_dev": "SELECT,INSERT", "agent_reader": "SELECT"} {
		var got string
		if err = conn.QueryRow(ctx, `SELECT COALESCE(string_agg(p, ',' ORDER BY p DESC),'') FROM unnest(ARRAY['SELECT','INSERT','UPDATE','DELETE','TRUNCATE']) p
		  WHERE has_table_privilege($1, 'agentrt.unit_run', p)`, role).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s may %s, want %s", role, got, want)
		}
	}
}
