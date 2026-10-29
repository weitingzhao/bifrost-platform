package migrate_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func must0010(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// A real 0009 database, with no new reader SQL used to create its tasks.
// Every fixture write uses the runtime role and the existing event guards.
func database0009(t *testing.T) (*agentrttest.DB, *pgx.Conn, []migrate.File) {
	t.Helper()
	db := agentrttest.New(t)
	ctx := context.Background()
	conn := db.Connect(t, db.Owner)
	files, err := migrate.Files()
	must0010(t, err)
	if len(files) < 10 || files[9].ID != 10 {
		t.Fatal("0010 is missing")
	}
	files = files[:10]
	applied, err := migrate.RunFiles(ctx, conn, "dev", files[:9])
	must0010(t, err)
	if !slices.Equal(applied, []int{1, 2, 3, 4, 5, 6, 7, 8, 9}) {
		t.Fatalf("0009 setup applied %v", applied)
	}
	_, err = store.Seed(ctx, conn)
	must0010(t, err)
	rt := db.Connect(t, db.Runtime)
	for host, kind := range map[string]string{"old-dev": "dev", "old-vm": "vm"} {
		must0010(t, store.RegisterHost(ctx, rt, "owner", host, kind))
	}
	must0010(t, store.InTx(ctx, rt, func(tx pgx.Tx) error {
		if _, err := store.CreateMission(ctx, tx, "controller", store.Mission{
			ID: "M-1", WorkID: "W-31", Title: "T50", Goal: "upgrade", Deadline: time.Now().Add(24 * time.Hour),
		}); err != nil {
			return err
		}
		return store.MoveMission(ctx, tx, "controller", "M-1", "active", "T50 fixture")
	}))
	must0010(t, store.InTx(ctx, rt, func(tx pgx.Tx) error {
		spec, err := store.PutArtifact(ctx, tx, "controller", "task_spec", "text/markdown", []byte("T50 ordinary task"))
		if err != nil {
			return err
		}
		if _, err = store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "T50 0009 fixture"}}); err != nil {
			return err
		}
		for _, state := range []string{"proposed", "running", "accepted"} {
			task := store.NewTaskID()
			if _, err = tx.Exec(ctx, `INSERT INTO agentrt.task
			  (task_id, mission_id, off_plan, off_plan_reason, role, task_class, title,
			   spec_artifact_id, effect_class, state, hard_limit_s, created_by_actor_id)
			  VALUES ($1, 'M-1', true, 'T50 fixture', 'implement', 'go', $2, $3, 'none', $2, 3600, 'controller')`,
				task, state, spec); err != nil {
				return err
			}
			if state == "proposed" {
				continue
			}
			attemptState, host := "running", "old-dev"
			if state == "accepted" {
				attemptState, host = "finished", "old-vm"
			}
			if _, err = tx.Exec(ctx, `INSERT INTO agentrt.attempt
			  (attempt_id, task_id, attempt_no, executor_actor_id, host_id, lease_epoch, lease_expires_at,
			   heartbeat_at, state, started_at, ended_at)
			  VALUES ($1, $2, 1, 'exec:codex', $3, nextval('agentrt.lease_epoch_seq'), now() + interval '1 hour',
			   now(), $4, now(), CASE WHEN $4 = 'finished' THEN now() END)`,
				store.NewAttemptID(), task, host, attemptState); err != nil {
				return err
			}
		}
		return nil
	}))
	return db, conn, files
}

// Compare every old column of every table (events, heads, outbox and migration
// records included), plus every sequence's last_value and is_called. Only the
// new nullable column and the new migration record are excluded after upgrade.
func data0010(t *testing.T, conn *pgx.Conn) map[string]string {
	t.Helper()
	ctx := context.Background()
	rows, err := conn.Query(ctx, `SELECT relname, relkind::text FROM pg_class
	  WHERE relnamespace = 'agentrt'::regnamespace AND relkind IN ('r','S') ORDER BY relname`)
	must0010(t, err)
	type relation struct{ name, kind string }
	rels, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (v relation, err error) {
		err = r.Scan(&v.name, &v.kind)
		return
	})
	must0010(t, err)
	out := map[string]string{}
	for _, rel := range rels {
		name := pgx.Identifier{"agentrt", rel.name}.Sanitize()
		q := `SELECT COALESCE(jsonb_agg(j ORDER BY j::text), '[]')::text FROM (SELECT to_jsonb(r) AS j FROM ` + name + ` r) s`
		switch {
		case rel.kind == "S":
			q = `SELECT jsonb_build_array(last_value, is_called)::text FROM ` + name
		case rel.name == "task":
			q = `SELECT COALESCE(jsonb_agg(j ORDER BY j::text), '[]')::text FROM
			  (SELECT to_jsonb(r) - 'executor_pin' AS j FROM agentrt.task r) s`
		case rel.name == "schema_migration":
			q = `SELECT jsonb_agg(to_jsonb(r) ORDER BY schema_migration_id)::text FROM agentrt.schema_migration r WHERE schema_migration_id <= 9`
		}
		var value string
		must0010(t, conn.QueryRow(ctx, q).Scan(&value))
		out[rel.name] = value
	}
	return out
}

// Object definitions, not OIDs: a rollback must restore all old constraints,
// columns, triggers, indexes, function definitions/ACLs and migration records.
func schema0010(t *testing.T, conn *pgx.Conn) string {
	t.Helper()
	var out string
	must0010(t, conn.QueryRow(context.Background(), `SELECT jsonb_build_object(
	  'constraints', (SELECT jsonb_agg(x ORDER BY x::text) FROM (SELECT jsonb_build_array(conrelid::regclass::text, conname, pg_get_constraintdef(oid)) x FROM pg_constraint WHERE connamespace = 'agentrt'::regnamespace) s),
	  'columns', (SELECT jsonb_agg(x ORDER BY x::text) FROM (SELECT jsonb_build_array(table_name, column_name, data_type, is_nullable, column_default) x FROM information_schema.columns WHERE table_schema = 'agentrt') s),
	  'triggers', (SELECT jsonb_agg(x ORDER BY x::text) FROM (SELECT pg_get_triggerdef(t.oid) x FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid WHERE c.relnamespace = 'agentrt'::regnamespace AND NOT t.tgisinternal) s),
	  'indexes', (SELECT jsonb_agg(indexdef ORDER BY indexdef) FROM pg_indexes WHERE schemaname = 'agentrt'),
	  'functions', (SELECT jsonb_agg(x ORDER BY x::text) FROM (SELECT jsonb_build_array(pg_get_functiondef(oid), proacl::text) x FROM pg_proc WHERE pronamespace = 'agentrt'::regnamespace) s),
	  'migrations', (SELECT jsonb_agg(to_jsonb(m) ORDER BY schema_migration_id) FROM agentrt.schema_migration m)
	)::text`).Scan(&out))
	return out
}

func assertSnapshot0010(t *testing.T, conn *pgx.Conn, schema string, data map[string]string) {
	t.Helper()
	if got := schema0010(t, conn); got != schema {
		t.Fatalf("failed migration changed schema/records\nbefore: %s\nafter: %s", schema, got)
	}
	if got := data0010(t, conn); !reflect.DeepEqual(got, data) {
		t.Fatal("migration changed old rows or sequences")
	}
}

func edited0010(files []migrate.File, sql string) []migrate.File {
	out := slices.Clone(files)
	out[9].SQL = sql
	out[9].SHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(sql)))
	return out
}

func upgrade0010(t *testing.T, conn *pgx.Conn, files []migrate.File) {
	t.Helper()
	applied, err := migrate.RunFiles(context.Background(), conn, "dev", files)
	must0010(t, err)
	if !slices.Equal(applied, []int{10}) {
		t.Fatalf("upgrade applied %v, want only 0010", applied)
	}
	var hash string
	must0010(t, conn.QueryRow(context.Background(), `SELECT sha256 FROM agentrt.schema_migration WHERE schema_migration_id=10`).Scan(&hash))
	if hash != files[9].SHA256 {
		t.Fatalf("0010 recorded hash %s, want %s", hash, files[9].SHA256)
	}
}

// T50: 0009 production structure + existing dev/vm/tasks/live/terminal
// attempts -> 0010; run actual 5a8edd55 code on this same upgraded database.
func TestUpgradeTo0010KeepsDataAndSupportsLegacyRuntime(t *testing.T) {
	t.Parallel()
	probe := buildLegacy0010(t) // compile even when the DB part must skip
	db, conn, files := database0009(t)
	before := data0010(t, conn)
	upgrade0010(t, conn, files)
	if after := data0010(t, conn); !reflect.DeepEqual(after, before) {
		t.Fatal("0010 changed an old column, event, stream head, migration record or sequence")
	}
	var total, unpinned int
	must0010(t, conn.QueryRow(context.Background(), `SELECT count(*), count(*) FILTER (WHERE executor_pin IS NULL) FROM agentrt.task`).Scan(&total, &unpinned))
	if total != 3 || unpinned != total {
		t.Fatalf("old tasks: total=%d NULL pins=%d", total, unpinned)
	}
	if again, err := migrate.RunFiles(context.Background(), conn, "dev", files); err != nil || len(again) != 0 {
		t.Fatalf("repeat migration: %v %v", again, err)
	}
	runLegacy0010(t, probe, db.Runtime)
	checkRuntime0010(t, db)
}
