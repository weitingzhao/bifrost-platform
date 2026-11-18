package migrate_test

import (
	"context"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
)

var dropped0036 = []string{"capability_score", "job_candidate", "job", "capability_fact", "budget_ledger", "vendor_pool", "metric_reading"}

// database0036 is a database migrated to 0035, its owner's connection, and
// the files up to and including 0036.
func database0036(t *testing.T) (*agentrttest.DB, *pgx.Conn, []migrate.File) {
	t.Helper()
	db := agentrttest.New(t)
	conn := db.Connect(t, db.Owner)
	files, err := migrate.Files()
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(files, func(f migrate.File) bool { return f.ID == 36 })
	if i < 0 {
		t.Fatal("0036 is missing")
	}
	if _, err = migrate.RunFiles(context.Background(), conn, "dev", files[:i]); err != nil {
		t.Fatal(err)
	}
	return db, conn, files[:i+1]
}

func tablesLeft0036(ctx context.Context, t *testing.T, conn *pgx.Conn) int {
	t.Helper()
	var n int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM pg_tables WHERE schemaname = 'agentrt' AND tablename = ANY($1)`, dropped0036).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// answers0036 is what the two functions 0036 rewrites answer for the tables
// that remain, over every event type in the vocabulary.
func answers0036(ctx context.Context, t *testing.T, conn *pgx.Conn) []string {
	t.Helper()
	rows, err := conn.Query(ctx, `
	  WITH ty AS (SELECT m[1] AS typ FROM regexp_matches(
	         (SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'event_type_closed'), '''([a-z_.]+)''', 'g') m),
	  tb AS (SELECT unnest(ARRAY['actor','host','plan_node','mission','task','task_dependency','attempt','message','control',
	         'artifact','evidence','unit_run','rule','no_such_table']) AS tbl),
	  bd AS (SELECT unnest(ARRAY['{}'::jsonb, '{"actors":["x"]}'::jsonb,
	         '{"actor_id":"x","host_id":"x","control_id":"x","message_id":"x"}'::jsonb]) AS body)
	  SELECT 'may ' || typ || ' ' || tbl || ' ' || agentrt.event_may_write(typ, tbl) FROM ty, tb
	  UNION ALL
	  SELECT 'names ' || tbl || ' ' || body::text || ' ' || agentrt.event_names_row(body, tbl, 'x') FROM tb, bd
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

// shape0036 is everything the rollback must bring back, one line per object:
// columns, constraints, indexes, triggers and grants of the seven tables, the
// identity sequence's grants, and the three functions.
func shape0036(ctx context.Context, t *testing.T, conn *pgx.Conn) []string {
	t.Helper()
	rows, err := conn.Query(ctx, `
	  WITH tb AS (SELECT c.oid, c.relname, c.relacl FROM pg_class c
	              WHERE c.relnamespace = 'agentrt'::regnamespace AND c.relname = ANY($1))
	  SELECT 'column ' || tb.relname || ' ' || a.attnum || ' ' || a.attname || ' ' || format_type(a.atttypid, a.atttypmod)
	         || ' notnull=' || a.attnotnull || ' identity=' || a.attidentity::text
	         || ' default=' || COALESCE(pg_get_expr(d.adbin, d.adrelid), '')
	    FROM tb JOIN pg_attribute a ON a.attrelid = tb.oid AND a.attnum > 0 AND NOT a.attisdropped
	    LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
	  UNION ALL
	  SELECT 'constraint ' || tb.relname || ' ' || k.conname || ' ' || pg_get_constraintdef(k.oid)
	    FROM tb JOIN pg_constraint k ON k.conrelid = tb.oid
	  UNION ALL
	  SELECT 'index ' || i.indexdef FROM pg_indexes i JOIN tb ON tb.relname = i.tablename WHERE i.schemaname = 'agentrt'
	  UNION ALL
	  SELECT 'trigger ' || pg_get_triggerdef(g.oid) FROM tb JOIN pg_trigger g ON g.tgrelid = tb.oid AND NOT g.tgisinternal
	  UNION ALL
	  SELECT 'grant ' || tb.relname || ' ' || x::text FROM tb, unnest(tb.relacl) x
	  UNION ALL
	  SELECT 'grant sequence ' || x::text FROM pg_class c, unnest(c.relacl) x
	   WHERE c.oid = 'agentrt.budget_ledger_budget_ledger_id_seq'::regclass
	  UNION ALL
	  SELECT 'function ' || p.proname || ' ' || pg_get_functiondef(p.oid) || ' acl=' || COALESCE(p.proacl::text, '')
	    FROM pg_proc p WHERE p.pronamespace = 'agentrt'::regnamespace
	     AND p.proname IN ('event_may_write', 'event_names_row', 'guard_metric_reading')
	  ORDER BY 1`, dropped0036)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// old0036 puts one row older than the boundary in each table with a time
// column and one in job, by the superuser with triggers and foreign keys off:
// no code can write these tables any more.
const old0036 = `
  INSERT INTO agentrt.metric_reading (metric_reading_id, metric_id, definition_version, definition_sha256, scope_kind, window_kind,
    window_start, window_end, source, unit, samples, conclusive, computed_from_event_id, computed_at)
    VALUES ('mr_01M7623S4JV0CRWKW50JFBQCZ8', 'lead_time_s', 1, repeat('a', 64), 'all', 'day', '2026-11-01', '2026-11-02', 'computed',
      'seconds', 0, false, 1, '2026-11-14 00:05:00+00');
  INSERT INTO agentrt.job (job_id, tenant, rule_id, rule_version) VALUES ('j1', 'dev', 'r', 1);
  INSERT INTO agentrt.job_candidate (job_candidate_id, job_id, executor_actor_id, model_provider, status)
    VALUES ('c1', 'j1', 'exec:codex', 'openai', 'active');
  INSERT INTO agentrt.capability_score (capability_score_id, executor_actor_id, model_provider, model, tool_version, job_id, task_class,
    core, source, window_start, window_end, attempts, first_pass, accepted, rework, score, computed_from_event_id, computed_at)
    VALUES ('s1', 'exec:codex', 'openai', 'm', '1', 'j1', 'go', false, 'exam', '2026-11-01', '2026-11-02', 1, 1, 1, 0, 0.5, 1, '2026-11-13 14:01:00+00');
  INSERT INTO agentrt.capability_fact (capability_fact_id, executor_actor_id, fact_kind, subject, source_kind, source, recorded_at)
    VALUES ('f1', 'exec:codex', 'cannot', 'x', 'measured', 't', '2026-11-08 00:00:00+00');
  INSERT INTO agentrt.vendor_pool (vendor_pool_id, tool, window_kind, measured_at, resets_at)
    VALUES ('p1', 'codex', 'short', '2026-11-13 03:00:00+00', '2026-12-01 00:00:00+00');
  INSERT INTO agentrt.budget_ledger (unit, amount, measured, at) VALUES ('wall_s', 1, true, '2026-11-13 14:00:00+00');`

// Origin: 2026-11-18, OWNER_LAW #37/#38: these tables hold data and no
// production code reads or writes them any more. "Nobody writes" was a
// measurement, and it must still hold when 0036 runs: a row written since, or
// more rows than either environment had, means a writer nobody found. Each
// case is the positive control of the drop: with its row 0036 refuses and all
// seven tables stay; with the row taken away the same file goes through.
func TestUpgradeTo0036RefusesTablesStillWritten(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, conn, files := database0036(t)
	admin := db.Connect(t, db.Admin)
	if _, err := admin.Exec(ctx, `SET session_replication_role = replica; `+old0036); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, put, take, want string }{
		{"metric_reading written at the boundary",
			`INSERT INTO agentrt.metric_reading (metric_reading_id, metric_id, definition_version, definition_sha256, scope_kind, window_kind,
			   window_start, window_end, source, unit, samples, conclusive, computed_from_event_id, computed_at)
			 VALUES ('mr_01M7623S4JV0CRWKW50JFBQCZ9', 'lead_time_s', 1, repeat('a', 64), 'all', 'day', '2026-11-03', '2026-11-04', 'computed',
			   'seconds', 0, false, 1, '2026-11-15 00:00:00+00')`,
			`DELETE FROM agentrt.metric_reading WHERE metric_reading_id = 'mr_01M7623S4JV0CRWKW50JFBQCZ9'`, "metric_reading"},
		{"capability_score written later",
			`INSERT INTO agentrt.capability_score (capability_score_id, executor_actor_id, model_provider, model, tool_version, job_id, task_class,
			   core, source, window_start, window_end, attempts, first_pass, accepted, rework, score, computed_from_event_id, computed_at)
			 VALUES ('s2', 'exec:codex', 'openai', 'm', '1', 'j1', 'go', false, 'exam', '2026-11-03', '2026-11-04', 1, 1, 1, 0, 0.5, 1, '2026-11-16 00:00:00+00')`,
			`DELETE FROM agentrt.capability_score WHERE capability_score_id = 's2'`, "capability_score"},
		{"budget_ledger written later",
			`INSERT INTO agentrt.budget_ledger (unit, amount, measured, at) VALUES ('usd', 2, true, '2026-11-16 00:00:00+00')`,
			`DELETE FROM agentrt.budget_ledger WHERE unit = 'usd'`, "budget_ledger"},
		{"vendor_pool measured later",
			`INSERT INTO agentrt.vendor_pool (vendor_pool_id, tool, window_kind, measured_at) VALUES ('p2', 'codex', 'weekly', '2026-11-16 00:00:00+00')`,
			`DELETE FROM agentrt.vendor_pool WHERE vendor_pool_id = 'p2'`, "vendor_pool"},
		{"capability_fact retired later",
			`INSERT INTO agentrt.capability_fact (capability_fact_id, executor_actor_id, fact_kind, subject, source_kind, source, recorded_at, retired_at)
			 VALUES ('f2', 'exec:codex', 'cannot', 'y', 'measured', 't', '2026-11-08 00:00:00+00', '2026-11-16 00:00:00+00')`,
			`DELETE FROM agentrt.capability_fact WHERE capability_fact_id = 'f2'`, "capability_fact"},
		{"job_candidate qualified later",
			`INSERT INTO agentrt.job_candidate (job_candidate_id, job_id, executor_actor_id, model_provider, status, qualified_at)
			 VALUES ('c2', 'j1', 'exec:claude', 'anthropic', 'active', '2026-11-16 00:00:00+00')`,
			`DELETE FROM agentrt.job_candidate WHERE job_candidate_id = 'c2'`, "job_candidate"},
		// job has no time column: only its number of rows can show a writer.
		{"more jobs than measured",
			`INSERT INTO agentrt.job (job_id, tenant, rule_id, rule_version) SELECT 'n' || i, 'dev', 'r', 1 FROM generate_series(1, 5) i`,
			`DELETE FROM agentrt.job WHERE job_id LIKE 'n%'`, "agentrt.job has 6 rows"},
		{"more pools than measured",
			`INSERT INTO agentrt.vendor_pool (vendor_pool_id, tool, window_kind) SELECT 'n' || i, 'codex', 'short' FROM generate_series(1, 4) i`,
			`DELETE FROM agentrt.vendor_pool WHERE vendor_pool_id LIKE 'n%'`, "agentrt.vendor_pool has 5 rows"},
	} {
		if _, err := admin.Exec(ctx, c.put); err != nil {
			t.Fatalf("%s: putting the row there: %v", c.name, err)
		}
		applied, err := migrate.RunFiles(ctx, conn, "dev", files)
		if err == nil || !strings.Contains(err.Error(), "nothing is dropped") || !strings.Contains(err.Error(), c.want) || len(applied) != 0 {
			t.Fatalf("%s: applied %v, %v", c.name, applied, err)
		}
		var record int
		if err = conn.QueryRow(ctx, `SELECT count(*) FROM agentrt.schema_migration WHERE schema_migration_id = 36`).Scan(&record); err != nil {
			t.Fatal(err)
		}
		if n := tablesLeft0036(ctx, t, conn); n != len(dropped0036) || record != 0 {
			t.Fatalf("%s: after the refusal %d of the tables are left, %d record of 0036", c.name, n, record)
		}
		if _, err = admin.Exec(ctx, c.take); err != nil {
			t.Fatalf("%s: taking the row away: %v", c.name, err)
		}
	}
	// Rows as old as the measured ones do not stop it.
	if applied, err := migrate.RunFiles(ctx, conn, "dev", files); err != nil || !slices.Equal(applied, []int{36}) {
		t.Fatalf("0036 once only old rows are left: applied %v, %v", applied, err)
	}
	if n := tablesLeft0036(ctx, t, conn); n != 0 {
		t.Fatalf("%d of the tables are left", n)
	}
}

// Origin: 2026-11-18, OWNER_LAW #37/#38: 0036 removes the seven tables nothing
// uses. The events stay and stay readable: no event row changes, the closed
// vocabulary is the same (budget_charged is one of the old types), the two
// shared functions answer as before for every table that remains, and the
// functions other tables use are still there. With ROLLBACK_0036 set to the
// rollback file, the rollback (without its \copy lines, the rows being the
// exports') brings back exactly what was there.
func TestUpgradeTo0036DropsTheDeadTables(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, conn, files := database0036(t)
	admin := db.Connect(t, db.Admin)
	if _, err := admin.Exec(ctx, `SET session_replication_role = replica;
	  INSERT INTO agentrt.event_stream (event_stream_id, last_seq, last_sha256) VALUES ('x:1', 1, repeat('0', 64));
	  INSERT INTO agentrt.event (event_stream_id, seq, event_type, actor_id, body_json, prev_sha256, sha256, at)
	  VALUES ('x:1', 1, 'budget_charged', 'controller', '{"vendor_pool_id":"p1"}', '', repeat('0', 64), now());`+old0036); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `SET session_replication_role = origin`); err != nil {
		t.Fatal(err)
	}
	const events = `SELECT count(*), md5(string_agg(e::text, E'\n' ORDER BY event_id)),
	  (SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'event_type_closed') FROM agentrt.event e`
	var n0, n1 int
	var sum0, sum1, vocab0, vocab1 string
	if err := conn.QueryRow(ctx, events).Scan(&n0, &sum0, &vocab0); err != nil {
		t.Fatal(err)
	}
	if n0 == 0 {
		t.Fatal("no event to compare")
	}
	answers := answers0036(ctx, t, conn)
	// The positive control of the comparison: both answers occur for each function.
	for _, fn := range []string{"may ", "names "} {
		if !slices.ContainsFunc(answers, func(s string) bool { return strings.HasPrefix(s, fn) && strings.HasSuffix(s, " true") }) ||
			!slices.ContainsFunc(answers, func(s string) bool { return strings.HasPrefix(s, fn) && strings.HasSuffix(s, " false") }) {
			t.Fatalf("the %sanswers compared lack a true or a false", fn)
		}
	}
	shape := shape0036(ctx, t, conn)
	if len(shape) < 100 {
		t.Fatalf("the shape compared has %d lines", len(shape))
	}

	if applied, err := migrate.RunFiles(ctx, conn, "dev", files); err != nil || !slices.Equal(applied, []int{36}) {
		t.Fatalf("0036: applied %v, %v", applied, err)
	}
	if n := tablesLeft0036(ctx, t, conn); n != 0 {
		t.Errorf("%d of the tables are left", n)
	}
	var naming, guard, shared, retired, leftovers int
	if err := conn.QueryRow(ctx, `SELECT
	  (SELECT count(*) FROM pg_proc WHERE pronamespace = 'agentrt'::regnamespace
	     AND prosrc ~ '(metric_reading|capability_score|budget_ledger|vendor_pool|capability_fact|job_candidate|''job'')'),
	  (SELECT count(*) FROM pg_proc WHERE pronamespace = 'agentrt'::regnamespace AND proname = 'guard_metric_reading'),
	  (SELECT count(*) FROM pg_proc WHERE pronamespace = 'agentrt'::regnamespace AND proname IN ('forbid_change', 'forbid_delete', 'require_event')),
	  (SELECT count(*) FROM pg_tables WHERE schemaname = 'agentrt' AND tablename = 'outbox_retired'),
	  (SELECT count(*) FROM pg_class WHERE relnamespace = 'agentrt'::regnamespace
	     AND relname ~ '^(metric_reading|capability_score|budget_ledger|vendor_pool|capability_fact|job_candidate|job)_')`).
		Scan(&naming, &guard, &shared, &retired, &leftovers); err != nil {
		t.Fatal(err)
	}
	if naming != 0 || guard != 0 || shared != 3 || retired != 1 || leftovers != 0 {
		t.Errorf("after 0036: %d functions name the tables, %d guard_metric_reading, %d of 3 shared functions, %d outbox_retired, %d indexes or sequences left",
			naming, guard, shared, retired, leftovers)
	}
	if err := conn.QueryRow(ctx, events).Scan(&n1, &sum1, &vocab1); err != nil {
		t.Fatal(err)
	}
	if n1 != n0 || sum1 != sum0 || vocab1 != vocab0 {
		t.Errorf("0036 changed the events: %d rows (were %d), same rows %v, same vocabulary %v", n1, n0, sum1 == sum0, vocab1 == vocab0)
	}
	if after := answers0036(ctx, t, conn); !slices.Equal(answers, after) {
		t.Errorf("the shared functions answer differently after 0036 for tables that remain")
	}

	path := os.Getenv("ROLLBACK_0036")
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sql := regexp.MustCompile(`(?m)^\\.*$`).ReplaceAllString(string(raw), "")
	sql = strings.ReplaceAll(sql, `:"runtime_role"`, `platform_agentrt_dev`)
	if _, err = conn.Exec(ctx, sql); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	after := shape0036(ctx, t, conn)
	for _, line := range shape {
		if !slices.Contains(after, line) {
			t.Errorf("the rollback did not bring back: %s", line)
		}
	}
	for _, line := range after {
		if !slices.Contains(shape, line) {
			t.Errorf("the rollback added: %s", line)
		}
	}
	if got := answers0036(ctx, t, conn); !slices.Equal(answers, got) {
		t.Errorf("the shared functions answer differently after the rollback")
	}
}
