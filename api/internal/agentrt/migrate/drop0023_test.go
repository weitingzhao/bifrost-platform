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

// database0023 is a database migrated to 0021, its owner's connection, and
// every migration file.
func database0023(t *testing.T) (*agentrttest.DB, *pgx.Conn, []migrate.File) {
	t.Helper()
	ctx := context.Background()
	db := agentrttest.New(t)
	conn := db.Connect(t, db.Owner)
	files, err := migrate.Files()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 23 || files[22].ID != 23 {
		t.Fatal("0023 is missing")
	}
	if _, err = migrate.RunFiles(ctx, conn, "dev", files[:22]); err != nil {
		t.Fatal(err)
	}
	return db, conn, files
}

// answers0023 is what the three shared functions 0023 rewrites answer, for
// every event type and table that remain after it: event_may_write for each
// pair, event_names_row for each table over bodies that name a row each way
// a body can, and only_liveness for each table over a change of a liveness
// column and a change of another.
func answers0023(ctx context.Context, t *testing.T, conn *pgx.Conn) []string {
	t.Helper()
	rows, err := conn.Query(ctx, `
	  WITH ty AS (SELECT m[1] AS typ FROM regexp_matches(
	         (SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'event_type_closed'), '''([a-z_]+)''', 'g') m
	       WHERE m[1] !~ '^(issue|intake)_'),
	  tb AS (SELECT unnest(ARRAY['actor','host','plan_node','mission','task','task_dependency','attempt','message','control',
	         'vendor_pool','job','job_candidate','capability_fact','artifact','evidence','unit_run','rule','no_such_table']) AS tbl),
	  bd AS (SELECT unnest(ARRAY['{}'::jsonb,
	         '{"actors":["x"],"capability_facts":["x"],"job_ids":["x"],"job_candidate_ids":["x"]}'::jsonb,
	         '{"actor_id":"x","host_id":"x","control_id":"x","vendor_pool_id":"x","message_id":"x","job_candidate_id":"x"}'::jsonb]) AS body),
	  ch AS (SELECT * FROM (VALUES ('{"last_poll_at":"a","k":1}'::jsonb, '{"last_poll_at":"b","k":1}'::jsonb),
	                               ('{"last_poll_at":"a","k":1}'::jsonb, '{"last_poll_at":"a","k":2}'::jsonb),
	                               ('{"signals_json":1}'::jsonb, '{"signals_json":2}'::jsonb)) AS c(o, n))
	  SELECT 'may ' || typ || ' ' || tbl || ' ' || agentrt.event_may_write(typ, tbl) FROM ty, tb
	  UNION ALL
	  SELECT 'names ' || tbl || ' ' || body::text || ' ' || agentrt.event_names_row(body, tbl, 'x') FROM tb, bd
	  UNION ALL
	  SELECT 'live ' || tbl || ' ' || o::text || ' ' || n::text || ' ' || agentrt.only_liveness(tbl, o, n) FROM tb, ch
	  ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// 2026-11-15: 0023 removes business issues from the database, on the Owner's
// word that day (relayed by the coordinator) that they are deleted, and on
// the measurement that their seven tables were empty on PROD and STG. The
// conditions set for it are held here, as for 0021: the tables, their
// functions and the two triggers on agentrt.mission are gone and the
// vocabulary no longer has their types; no event row changed; and the three
// shared functions it rewrites answer as before for every type and table
// that remain.
func TestUpgradeTo0023DropsBusinessIssues(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, conn, files := database0023(t)
	before := answers0023(ctx, t, conn)
	// The positive control of the comparison: it has both kinds of answer,
	// for each of the three functions.
	for _, fn := range []string{"may ", "names ", "live "} {
		if !slices.ContainsFunc(before, func(s string) bool { return strings.HasPrefix(s, fn) && strings.HasSuffix(s, " true") }) ||
			!slices.ContainsFunc(before, func(s string) bool { return strings.HasPrefix(s, fn) && strings.HasSuffix(s, " false") }) {
			t.Fatalf("the %sanswers compared lack a true or a false", fn)
		}
	}
	if len(before) < 1000 {
		t.Fatalf("the answers compared are %d", len(before))
	}
	var events0 int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM agentrt.event`).Scan(&events0); err != nil {
		t.Fatal(err)
	}
	applied, err := migrate.RunFiles(ctx, conn, "dev", files[:23])
	if err != nil || !slices.Equal(applied, []int{23}) {
		t.Fatalf("0023 on empty business issues: applied %v, %v", applied, err)
	}
	var tables, functions, triggers, types, events int
	if err = conn.QueryRow(ctx, `SELECT
	  (SELECT count(*) FROM pg_tables WHERE schemaname = 'agentrt' AND tablename ~ '^(issue|issue_text|issue_ask|issue_check|issue_work|intake_client|person)$'),
	  (SELECT count(*) FROM pg_proc WHERE pronamespace = 'agentrt'::regnamespace AND (proname ~ 'issue|intake|person' OR prosrc ~ 'issue|intake|person')),
	  (SELECT count(*) FROM pg_trigger WHERE tgrelid = 'agentrt.mission'::regclass AND tgname ~ 'issue'),
	  (SELECT count(*) FROM regexp_matches((SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'event_type_closed'),
	     '''(issue|intake)_[a-z_]+''', 'g')),
	  (SELECT count(*) FROM agentrt.event)`).Scan(&tables, &functions, &triggers, &types, &events); err != nil {
		t.Fatal(err)
	}
	if tables != 0 || functions != 0 || triggers != 0 || types != 0 {
		t.Errorf("after 0023: %d issue tables, %d functions naming them, %d of their triggers on mission, %d of their types in the vocabulary", tables, functions, triggers, types)
	}
	if events != events0 {
		t.Errorf("0023 changed the number of events: %d, was %d", events, events0)
	}
	if after := answers0023(ctx, t, conn); !slices.Equal(before, after) {
		for i := range before {
			if i >= len(after) || before[i] != after[i] {
				t.Fatalf("the shared functions answer differently after 0023: was %q, is %q", before[i], after[min(i, len(after)-1)])
			}
		}
		t.Fatalf("the shared functions give %d answers after 0023, gave %d", len(after), len(before))
	}
}

// The refusals are the helpers' (migration 0022, the CI line's) and the
// narrowed constraint's; this test holds only that each stops the file and
// names what stopped it.
//
// 0023 looks before it acts ("empty" was a measurement of 2026-11-15 and must
// hold when it runs; the same condition the coordinator set for 0021). A row
// in one of the tables, an event of one of their types, an event on one of
// their streams, or a mission with a business issue's work id, and it
// refuses: the whole file rolls back, nothing is dropped and no record 23 is
// written. With the row gone it applies. The rows are put there by the
// superuser with triggers off, since no code can write them any more.
func TestUpgradeTo0023RefusesUsedBusinessIssues(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct{ put, take, want string }{
		"a row in one of the tables": {
			`INSERT INTO agentrt.person (person_id, display_name) VALUES ('person:p1', 'p')`,
			`DELETE FROM agentrt.person`,
			"person"},
		"an event of an issue type": {
			`INSERT INTO agentrt.event_stream (event_stream_id, last_seq, last_sha256) VALUES ('x:1', 1, repeat('0', 64));
			 INSERT INTO agentrt.event (event_stream_id, seq, event_type, actor_id, body_json, prev_sha256, sha256, at)
			 VALUES ('x:1', 1, 'issue_replied', 'controller', '{}', '', repeat('0', 64), now())`,
			`DELETE FROM agentrt.event WHERE event_type = 'issue_replied'; DELETE FROM agentrt.event_stream WHERE event_stream_id = 'x:1'`,
			"event_type_closed"},
		// A type that stays in the vocabulary, on one of their streams.
		"an event on an issue stream": {
			`INSERT INTO agentrt.event_stream (event_stream_id, last_seq, last_sha256) VALUES ('issue:BI-1', 1, repeat('0', 64));
			 INSERT INTO agentrt.event (event_stream_id, seq, event_type, actor_id, body_json, prev_sha256, sha256, at)
			 VALUES ('issue:BI-1', 1, 'note', 'controller', '{}', '', repeat('0', 64), now())`,
			`DELETE FROM agentrt.event WHERE event_stream_id = 'issue:BI-1'; DELETE FROM agentrt.event_stream WHERE event_stream_id = 'issue:BI-1'`,
			"issue:"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			db, conn, files := database0023(t)
			admin := db.Connect(t, db.Admin)
			if _, err := admin.Exec(ctx, `SET session_replication_role = replica; `+c.put); err != nil {
				t.Fatalf("putting the row there: %v", err)
			}
			applied, err := migrate.RunFiles(ctx, conn, "dev", files[:23])
			if err == nil || !strings.Contains(err.Error(), c.want) || len(applied) != 0 {
				t.Fatalf("0023 on used business issues: applied %v, %v", applied, err)
			}
			var tables, record int
			if err = conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM pg_tables WHERE schemaname = 'agentrt' AND tablename ~ '^(issue|issue_text|issue_ask|issue_check|issue_work|intake_client|person)$'),
			  (SELECT count(*) FROM agentrt.schema_migration WHERE schema_migration_id = 23)`).Scan(&tables, &record); err != nil {
				t.Fatal(err)
			}
			if tables != 7 || record != 0 {
				t.Fatalf("after the refusal: %d issue tables (7 before), %d record of 0023", tables, record)
			}
			if _, err = admin.Exec(ctx, c.take); err != nil {
				t.Fatalf("taking the row away: %v", err)
			}
			if applied, err = migrate.RunFiles(ctx, conn, "dev", files[:23]); err != nil || !slices.Equal(applied, []int{23}) {
				t.Fatalf("0023 once business issues are empty again: applied %v, %v", applied, err)
			}
		})
	}
}
