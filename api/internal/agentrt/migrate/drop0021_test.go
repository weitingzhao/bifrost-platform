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

// database0020 is a database migrated to 0020, its owner's connection, and
// every migration file; the tests below run them through 0021 (files[:21]).
func database0020(t *testing.T) (*agentrttest.DB, *pgx.Conn, []migrate.File) {
	t.Helper()
	ctx := context.Background()
	db := agentrttest.New(t)
	conn := db.Connect(t, db.Owner)
	files, err := migrate.Files()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 21 || files[20].ID != 21 {
		t.Fatal("0021 is missing")
	}
	if _, err = migrate.RunFiles(ctx, conn, "dev", files[:20]); err != nil {
		t.Fatal(err)
	}
	return db, conn, files
}

// answers0021 is what the two shared functions 0021 rewrites answer, for
// every event type and table that remain after it: event_may_write for each
// pair, event_names_row for each table over bodies that name a row each way
// a body can.
func answers0021(ctx context.Context, t *testing.T, conn *pgx.Conn) []string {
	t.Helper()
	rows, err := conn.Query(ctx, `
	  WITH ty AS (SELECT m[1] AS typ FROM regexp_matches(
	         (SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'event_type_closed'), '''([a-z_]+)''', 'g') m
	       WHERE m[1] !~ '^(debt|gap|ratchet)_'),
	  tb AS (SELECT unnest(ARRAY['actor','host','plan_node','mission','task','task_dependency','attempt','message','control',
	         'vendor_pool','job','job_candidate','capability_fact','person','intake_client','issue','issue_text','issue_ask',
	         'issue_check','issue_work','artifact','evidence','unit_run','rule','no_such_table']) AS tbl),
	  bd AS (SELECT unnest(ARRAY['{}'::jsonb,
	         '{"actors":["x"],"capability_facts":["x"],"job_ids":["x"],"job_candidate_ids":["x"]}'::jsonb,
	         '{"actor_id":"x","host_id":"x","control_id":"x","vendor_pool_id":"x","message_id":"x","job_candidate_id":"x","person_id":"x","intake_client_id":"x","issue_id":"x","check_id":"7","text_id":"7","ask_id":"7"}'::jsonb,
	         '{"text_ids":[7],"ask_ids":[7]}'::jsonb]) AS body)
	  SELECT 'may ' || typ || ' ' || tbl || ' ' || agentrt.event_may_write(typ, tbl) FROM ty, tb
	  UNION ALL
	  SELECT 'names ' || tbl || ' ' || id || ' ' || body::text || ' ' || agentrt.event_names_row(body, tbl, id)
	    FROM tb, bd, (VALUES ('x'), ('7')) AS i(id) WHERE NOT (id = 'x' AND tbl IN ('issue_text','issue_ask'))
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

// 2026-11-14: 0021 removes the debt ledger from the database, on the
// measurement that it was empty and unused. The coordinator's conditions for
// it are held here. The ledger's tables, its functions and its two triggers
// on agentrt.event are gone and the vocabulary no longer has its types; no
// event row changed; and the two shared functions it rewrites answer as
// before for every type and table that remain.
func TestUpgradeTo0021DropsTheDebtLedger(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, conn, files := database0020(t)
	before := answers0021(ctx, t, conn)
	// The positive control of the comparison: it has both kinds of answer.
	if len(before) < 2000 || !slices.ContainsFunc(before, func(s string) bool { return strings.HasSuffix(s, " true") }) ||
		!slices.ContainsFunc(before, func(s string) bool { return strings.HasSuffix(s, " false") }) {
		t.Fatalf("the answers compared are %d, without both a true and a false", len(before))
	}
	var events0 int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM agentrt.event`).Scan(&events0); err != nil {
		t.Fatal(err)
	}
	applied, err := migrate.RunFiles(ctx, conn, "dev", files[:21])
	if err != nil || !slices.Equal(applied, []int{21}) {
		t.Fatalf("0021 on an empty ledger: applied %v, %v", applied, err)
	}
	var tables, functions, triggers, types, events int
	if err = conn.QueryRow(ctx, `SELECT
	  (SELECT count(*) FROM pg_tables WHERE schemaname = 'agentrt' AND tablename ~ '^(debt|ratchet$|accepted_gap$)'),
	  (SELECT count(*) FROM pg_proc WHERE pronamespace = 'agentrt'::regnamespace AND (proname ~ 'debt' OR prosrc ~* 'debt|ratchet|accepted_gap')),
	  (SELECT count(*) FROM pg_trigger WHERE tgrelid = 'agentrt.event'::regclass AND tgname ~ 'debt'),
	  (SELECT count(*) FROM regexp_matches((SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'event_type_closed'),
	     '''(debt|gap|ratchet)_[a-z_]+''', 'g')),
	  (SELECT count(*) FROM agentrt.event)`).Scan(&tables, &functions, &triggers, &types, &events); err != nil {
		t.Fatal(err)
	}
	if tables != 0 || functions != 0 || triggers != 0 || types != 0 {
		t.Errorf("after 0021: %d ledger tables, %d functions naming it, %d of its triggers on event, %d of its types in the vocabulary", tables, functions, triggers, types)
	}
	if events != events0 {
		t.Errorf("0021 changed the number of events: %d, was %d", events, events0)
	}
	if after := answers0021(ctx, t, conn); !slices.Equal(before, after) {
		for i := range before {
			if i >= len(after) || before[i] != after[i] {
				t.Fatalf("the shared functions answer differently after 0021: was %q, is %q", before[i], after[min(i, len(after)-1)])
			}
		}
		t.Fatalf("the shared functions give %d answers after 0021, gave %d", len(after), len(before))
	}
}

// 0021 looks before it acts (the coordinator's first condition, 2026-11-14:
// "empty" was a measurement of that day and must hold when it runs). A row in
// a ledger table, an event of one of the ledger's types, or any event on one
// of its streams (added on the duty coordinator's review), and it refuses:
// the whole file rolls back, nothing is dropped and no record 21 is written.
// With the row gone it applies. The rows are put there by the superuser with
// triggers off, since no code can write them any more.
func TestUpgradeTo0021RefusesAUsedLedger(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct{ put, take, want string }{
		"a row in a ledger table": {
			`INSERT INTO agentrt.accepted_gap (accepted_gap_id, project, gap_kind, title, reason, reopen_when, state)
			 VALUES ('AG-1', 'crew', 'accepted_risk', 't', 'r', 'w', 'accepted')`,
			`DELETE FROM agentrt.accepted_gap`,
			"agentrt.accepted_gap has 1 row(s)"},
		"an event of a ledger type": {
			`INSERT INTO agentrt.event_stream (event_stream_id, last_seq, last_sha256) VALUES ('gap:AG-1', 1, repeat('0', 64));
			 INSERT INTO agentrt.event (event_stream_id, seq, event_type, actor_id, body_json, prev_sha256, sha256, at)
			 VALUES ('gap:AG-1', 1, 'gap_accepted', 'controller', '{}', '', repeat('0', 64), now())`,
			`DELETE FROM agentrt.event WHERE event_type = 'gap_accepted'; DELETE FROM agentrt.event_stream WHERE event_stream_id = 'gap:AG-1'`,
			"1 event(s) of the debt ledger's types exist"},
		// A type that stays in the vocabulary, on one of the ledger's streams.
		"an event on a ledger stream": {
			`INSERT INTO agentrt.event_stream (event_stream_id, last_seq, last_sha256) VALUES ('debt:TD-1', 1, repeat('0', 64));
			 INSERT INTO agentrt.event (event_stream_id, seq, event_type, actor_id, body_json, prev_sha256, sha256, at)
			 VALUES ('debt:TD-1', 1, 'note', 'controller', '{}', '', repeat('0', 64), now())`,
			`DELETE FROM agentrt.event WHERE event_stream_id = 'debt:TD-1'; DELETE FROM agentrt.event_stream WHERE event_stream_id = 'debt:TD-1'`,
			"1 event(s) on a debt: stream exist"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			db, conn, files := database0020(t)
			admin := db.Connect(t, db.Admin)
			if _, err := admin.Exec(ctx, `SET session_replication_role = replica; `+c.put); err != nil {
				t.Fatalf("putting the row there: %v", err)
			}
			applied, err := migrate.RunFiles(ctx, conn, "dev", files[:21])
			if err == nil || !strings.Contains(err.Error(), c.want) || len(applied) != 0 {
				t.Fatalf("0021 on a used ledger: applied %v, %v", applied, err)
			}
			var tables, record int
			if err = conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM pg_tables WHERE schemaname = 'agentrt' AND tablename ~ '^(debt|ratchet$|accepted_gap$)'),
			  (SELECT count(*) FROM agentrt.schema_migration WHERE schema_migration_id = 21)`).Scan(&tables, &record); err != nil {
				t.Fatal(err)
			}
			if tables != 16 || record != 0 {
				t.Fatalf("after the refusal: %d ledger tables (16 before), %d record of 0021", tables, record)
			}
			if _, err = admin.Exec(ctx, c.take); err != nil {
				t.Fatalf("taking the row away: %v", err)
			}
			if applied, err = migrate.RunFiles(ctx, conn, "dev", files[:21]); err != nil || !slices.Equal(applied, []int{21}) {
				t.Fatalf("0021 once the ledger is empty again: applied %v, %v", applied, err)
			}
		})
	}
}
