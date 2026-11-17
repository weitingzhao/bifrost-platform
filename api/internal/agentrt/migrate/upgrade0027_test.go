package migrate_test

import (
	"context"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
)

// Origin: 2026-11-16, moving the release records out of ConfigMaps (shared
// notes duty/draft-ci-w1-2026-11-16-release-record-table.md). Review 162 A1
// was the gap the table closes: a delivery that failed was known only while
// Tekton kept its run, so the newest record could be taken for the current
// deployment. 0027 adds the table and touches nothing else: a failed run is a
// row; a record built again is a second row of the same run; an environment
// that cannot be a bus subject segment is refused; a row is never changed or
// removed.
func TestUpgrade0027AddsAppendOnlyReleaseRecords(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := agentrttest.New(t)
	conn := db.Connect(t, db.Owner)
	files, err := migrate.Files()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = migrate.RunFiles(ctx, conn, "dev", files[:26]); err != nil {
		t.Fatal(err)
	}
	if applied, err := migrate.RunFiles(ctx, conn, "dev", files[:27]); err != nil || len(applied) != 1 || applied[0] != 27 {
		t.Fatalf("applied %v: %v", applied, err)
	}
	const insert = `INSERT INTO agentrt.release_record (run, pipeline, lane, env, deploys, succeeded, started_at, completed_at, repos, missing, record_version)
	  VALUES ($1,'bifrost-deliver-platform','platform',$2,true,$3,now() - interval '3 minutes',now(),$4::jsonb,$5::jsonb,'v4')`
	repos := `{"bifrost-platform":{"sha":"` + "0123456789012345678901234567890123456789" + `","source":"result"}}`
	rows := []struct {
		run, env  string
		succeeded bool
		missing   string
	}{
		{"bifrost-deliver-platform-1", "stg", true, `["clone-ui"]`}, // incomplete
		{"bifrost-deliver-platform-1", "stg", true, `[]`},           // the same run, built again
		{"bifrost-deliver-platform-2", "stg", false, `[]`},          // a delivery that failed
	}
	for _, r := range rows {
		if _, err = conn.Exec(ctx, insert, r.run, r.env, r.succeeded, repos, r.missing); err != nil {
			t.Fatalf("%s: %v", r.run, err)
		}
	}
	// The record of a run is its newest row; the tenant defaults to the platform's own.
	var missing, tenant string
	if err = conn.QueryRow(ctx, `SELECT missing::text, tenant FROM agentrt.release_record WHERE run = 'bifrost-deliver-platform-1'
	  ORDER BY release_record_id DESC LIMIT 1`).Scan(&missing, &tenant); err != nil || missing != "[]" || tenant != "rocket" {
		t.Fatalf("newest row of the run: missing %s tenant %s: %v", missing, tenant, err)
	}
	// What followed the deployment: the failed run is there to be seen.
	var after int
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM agentrt.release_record r WHERE lane = 'platform' AND env = 'stg' AND completed_at >=
	  (SELECT max(completed_at) FROM agentrt.release_record WHERE run = 'bifrost-deliver-platform-1') AND run <> 'bifrost-deliver-platform-1'`).Scan(&after); err != nil || after != 1 {
		t.Fatalf("deliveries after the deployment: %d, %v", after, err)
	}
	for name, stmt := range map[string]string{
		"an environment that is no subject segment": `INSERT INTO agentrt.release_record (run, pipeline, lane, env, deploys, succeeded, started_at, completed_at, record_version)
		  VALUES ('r','p','platform','STG',true,true,now(),now(),'v4')`,
		"a run that ended before it began": `INSERT INTO agentrt.release_record (run, pipeline, lane, env, deploys, succeeded, started_at, completed_at, record_version)
		  VALUES ('r','p','platform','stg',true,true,now(),now() - interval '1 second','v4')`,
		"repos that are not an object": `INSERT INTO agentrt.release_record (run, pipeline, lane, env, deploys, succeeded, started_at, completed_at, repos, record_version)
		  VALUES ('r','p','platform','stg',true,true,now(),now(),'[]','v4')`,
		"an event that is not in the ledger": `INSERT INTO agentrt.release_record (run, pipeline, lane, env, deploys, succeeded, started_at, completed_at, record_version, event_id)
		  VALUES ('r','p','platform','stg',true,true,now(),now(),'v4',987654321)`,
		"a change of a row":  `UPDATE agentrt.release_record SET succeeded = true WHERE run = 'bifrost-deliver-platform-2'`,
		"a removal of a row": `DELETE FROM agentrt.release_record WHERE run = 'bifrost-deliver-platform-2'`,
		"a truncate":         `TRUNCATE agentrt.release_record`,
	} {
		if _, err = conn.Exec(ctx, stmt); err == nil {
			t.Errorf("%s was taken", name)
		}
	}
	var n int
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM agentrt.release_record`).Scan(&n); err != nil || n != 3 {
		t.Fatalf("rows after the refusals: %d, %v", n, err)
	}
}
