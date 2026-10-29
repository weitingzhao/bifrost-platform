package migrate_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
)

func pgError0010(t *testing.T, err error, code, message string) {
	t.Helper()
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != code || !strings.Contains(pg.Message, message) {
		t.Fatalf("error=%v, want SQLSTATE %s containing %q", err, code, message)
	}
}

// Both a failure after the DROP/ADD and new triggers, and a failure of the
// migrator's own bookkeeping INSERT must roll back ALL objects and records.
func TestUpgradeTo0010FailureRollsBack(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"after triggers", "migration record"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			_, conn, files := database0009(t)
			schema, data := schema0010(t, conn), data0010(t, conn)
			sql, code, message := files[9].SQL, "22012", "division by zero"
			if stage == "after triggers" {
				const anchor = "CREATE INDEX task_pinned_ready_idx"
				if strings.Count(sql, anchor) != 1 {
					t.Fatal("failure injection anchor changed")
				}
				sql = strings.Replace(sql, anchor, "SELECT 1/0;\n"+anchor, 1)
			} else {
				sql += "\nINSERT INTO agentrt.schema_migration(schema_migration_id, sha256) VALUES (10, repeat('0', 64));"
				code, message = "23505", "schema_migration_pkey"
			}
			applied, err := migrate.RunFiles(context.Background(), conn, "dev", edited0010(files, sql))
			pgError0010(t, err, code, message)
			if len(applied) != 0 {
				t.Fatalf("failed file reported applied: %v", applied)
			}
			assertSnapshot0010(t, conn, schema, data)
			upgrade0010(t, conn, files) // retry uses the original bytes/hash
		})
	}
}

// Long transactions block task's ALTER and attempt's CREATE TRIGGER. In
// each case the migration already holds host's ACCESS EXCLUSIVE lock.
func TestUpgradeTo0010LockTimeoutRollsBack(t *testing.T) {
	t.Parallel()
	for _, table := range []string{"task", "attempt"} {
		t.Run(table, func(t *testing.T) {
			t.Parallel()
			db, conn, files := database0009(t)
			schema, data := schema0010(t, conn), data0010(t, conn)
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			holder := db.Connect(t, db.Runtime)
			tx, err := holder.Begin(ctx)
			must0010(t, err)
			defer func() { _ = tx.Rollback(context.Background()) }()
			lock := `SELECT task_id FROM agentrt.task` // ACCESS SHARE until rollback
			if table == "attempt" {
				lock = `LOCK TABLE agentrt.attempt IN ROW EXCLUSIVE MODE`
			}
			_, err = tx.Exec(ctx, lock)
			must0010(t, err)
			observer := db.Connect(t, db.Admin)
			done := make(chan error, 1)
			started := time.Now()
			go func() {
				_, err := migrate.RunFiles(ctx, conn, "dev", files)
				done <- err
			}()
			// Synchronize on actual lock state, not a guessed sleep.
			for {
				var waiting, holdsHost bool
				must0010(t, observer.QueryRow(ctx, `SELECT
				  EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND relation=('agentrt.' || $2)::regclass AND NOT granted),
				  EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND relation='agentrt.host'::regclass AND granted AND mode='AccessExclusiveLock')`,
					conn.PgConn().PID(), table).Scan(&waiting, &holdsHost))
				if waiting && holdsHost {
					break
				}
				select {
				case err := <-done:
					t.Fatalf("migration ended before staged lock contention: %v", err)
				case <-ctx.Done():
					t.Fatal("migration did not reach the expected lock wait")
				case <-time.After(5 * time.Millisecond):
				}
			}
			err = <-done
			elapsed := time.Since(started)
			pgError0010(t, err, "55P03", "lock timeout")
			if elapsed < time.Second || elapsed > 6*time.Second {
				t.Fatalf("lock wait elapsed %v, want approximately 2s, below outer deadline", elapsed)
			}
			var locks int
			must0010(t, observer.QueryRow(context.Background(), `SELECT count(*) FROM pg_locks WHERE pid=$1
			  AND relation IN ('agentrt.host'::regclass,'agentrt.task'::regclass,'agentrt.attempt'::regclass)`, conn.PgConn().PID()).Scan(&locks))
			if locks != 0 {
				t.Fatalf("migration retained %d relation locks after timeout", locks)
			}
			// Check while the original long transaction is STILL open.
			assertSnapshot0010(t, conn, schema, data)
			must0010(t, tx.Rollback(context.Background()))
			upgrade0010(t, conn, files)
			t.Logf("%s contention exited after %v; all DDL/records/locks rolled back", table, elapsed)
		})
	}
}

// Exercise the production statement budget, not a shorter test-only setting.
// This also proves settings are LOCAL and restored on both failure and success.
func TestUpgradeTo0010StatementTimeoutRollsBack(t *testing.T) {
	t.Parallel()
	_, conn, files := database0009(t)
	schema, data := schema0010(t, conn), data0010(t, conn)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	_, err := conn.Exec(ctx, `SET lock_timeout='11s'; SET statement_timeout='37s'`)
	must0010(t, err)
	checkSession := func() {
		t.Helper()
		var lock, stmt string
		must0010(t, conn.QueryRow(context.Background(), `SELECT current_setting('lock_timeout'), current_setting('statement_timeout')`).Scan(&lock, &stmt))
		if lock != "11s" || stmt != "37s" {
			t.Fatalf("migration leaked settings into session: %s %s", lock, stmt)
		}
	}
	started := time.Now()
	_, err = migrate.RunFiles(ctx, conn, "dev", edited0010(files, files[9].SQL+"\nSELECT pg_sleep(60);"))
	elapsed := time.Since(started)
	pgError0010(t, err, "57014", "statement timeout")
	if elapsed < 29*time.Second || elapsed > 35*time.Second {
		t.Fatalf("statement took %v, want production 30s budget", elapsed)
	}
	assertSnapshot0010(t, conn, schema, data)
	checkSession()
	upgrade0010(t, conn, files)
	checkSession()
}
