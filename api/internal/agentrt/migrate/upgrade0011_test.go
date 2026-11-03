package migrate_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// A lock 0011 waits on: the table held and the statement that holds it.
type lockCase0011 struct{ table, lock string }

// The STG step's lock check for 0011 (R5:5.8-3a). A long transaction holds a
// table that 0011 alters or references, so 0011 waits out its lock_timeout
// (2s, SET LOCAL in migrate.apply). The whole file then rolls back while that
// transaction is still open: there is no debt table and no record 11, 0011's
// connection keeps no relation or advisory lock, and what snapshot0011 reads
// (the definitions schema0010 lists; the ACLs of the schema, its functions,
// relations and columns and the default privileges; trigger states; every
// row) is as 0010 left it. Once the transaction ends, 0011 applies cleanly.
//
// Both waits come early in the file, before 0011 replaces 0010's functions
// (event_may_write, event_names_row) and adds its triggers on event and its
// grants: nothing later in it waits on a table another session can hold (it
// touches only its own tables and event, which it holds by then). A failure
// that late is TestUpgradeTo0011FailureRollsBack's (debt0011_test.go).
func TestUpgradeTo0011LockTimeoutRollsBack(t *testing.T) {
	t.Parallel()
	for _, c := range []lockCase0011{
		// 0011's first statement replaces the event vocabulary (ALTER TABLE
		// agentrt.event): any open reader of events blocks it before 0011 has
		// changed anything.
		{"event", `SELECT count(*) FROM agentrt.event`},
		// debt_work's foreign key to mission: an open writer of missions
		// blocks it after 0011 has replaced the event vocabulary and created
		// its first tables and functions of its own (debt_counter and the ones
		// before debt_work), and before it replaces 0010's functions.
		{"mission", `LOCK TABLE agentrt.mission IN ROW EXCLUSIVE MODE`},
	} {
		t.Run(c.table, func(t *testing.T) {
			t.Parallel()
			// The mission case's evidence is the lock state while 0011 waits,
			// polled with 5 ms between reads during its 2 s wait. A run whose
			// wait ended unseen (the observer not scheduled in time) has still
			// passed every rollback check; it is done again on a fresh database,
			// three runs at most, and one of them must see the wait (this makes
			// a miss unlikely, not impossible).
			for run := 1; ; run++ {
				if lockTimeout0011(t, c) {
					return
				}
				if run == 3 {
					t.Fatalf("the wait for %s was not seen in %d runs", c.table, run)
				}
				t.Logf("run %d: the wait for %s ended before it was seen; again on a fresh database", run, c.table)
			}
		})
	}
}

// One run of a lock case on a fresh 0010 database: every check, and whether
// the wait was seen (the first statement's needs no sighting: a lock timeout
// is that wait).
func lockTimeout0011(t *testing.T, c lockCase0011) bool {
	t.Helper()
	db, conn, files := database0009(t)
	upgrade0010(t, conn, files)
	pinned0010(t, db, conn)
	all, err := migrate.Files()
	must0010(t, err)
	if len(all) < 11 || all[10].ID != 11 {
		t.Fatal("0011 is missing")
	}
	files = all[:11]
	holder := db.Connect(t, db.Runtime)
	observer := db.Connect(t, db.Admin)
	// The run's budget starts once its connections are open: the snapshots,
	// the wait, the checks and the retry are all under it. Ending a migration
	// run is run0011.end's; the connections' own cleanup (t.Cleanup) is the
	// test helpers' and has no bound here.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	before := snapshot0011(ctx, t, conn)
	tx, err := holder.Begin(ctx)
	must0010(t, err)
	release := func(bound time.Duration) {
		end, stop := context.WithTimeout(context.Background(), bound)
		defer stop()
		_ = tx.Rollback(end)
	}
	defer release(10 * time.Second)
	_, err = tx.Exec(ctx, c.lock)
	must0010(t, err)
	started := time.Now()
	run := start0011(ctx, conn, files)
	defer run.end(t, cancel, func() { release(5 * time.Second) })
	done := run.done
	// On the lock state, not a guessed sleep: 0011 waits for the table. In the
	// mission case it already holds event exclusively and holds locks on
	// relations of its own, which no other session can see (their pg_class
	// rows are not committed); invisible counts those lock rows (tables, their
	// indexes, sequences), not tables.
	var invisible int
	var result run0011Result
	seen, ended := false, false
	for !seen && !ended {
		var waiting, holdsEvent bool
		must0010(t, observer.QueryRow(ctx, `SELECT
		  EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND relation=('agentrt.' || $2)::regclass AND NOT granted),
		  EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND relation='agentrt.event'::regclass AND granted AND mode='AccessExclusiveLock'),
		  (SELECT count(*) FROM pg_locks l WHERE l.pid=$1 AND l.locktype='relation' AND l.granted
		     AND NOT EXISTS (SELECT 1 FROM pg_class c WHERE c.oid = l.relation))`,
			conn.PgConn().PID(), c.table).Scan(&waiting, &holdsEvent, &invisible))
		if waiting && (c.table == "event" || holdsEvent && invisible > 0) {
			seen = true
			break
		}
		select {
		case result = <-done:
			if !lockTimeout(result.err) {
				t.Fatalf("0011 ended before the wait for %s was seen: %v", c.table, result.err)
			}
			ended = true
		case <-ctx.Done():
			t.Fatalf("0011 never waited for %s within the run's budget", c.table)
		case <-time.After(5 * time.Millisecond):
		}
	}
	if !ended {
		select {
		case result = <-done:
		case <-ctx.Done():
			t.Fatalf("0011 did not end within the run's budget while %s was held", c.table)
		}
	}
	elapsed := time.Since(started)
	pgError0010(t, result.err, "55P03", "lock timeout")
	// A lock timeout comes 2 s after the wait began, and the wait began after
	// RunFiles did: never sooner than that from started.
	if elapsed < 2*time.Second-50*time.Millisecond {
		t.Fatalf("a lock timeout after %v, sooner than 0011's 2 s lock_timeout", elapsed)
	}
	var locks int
	must0010(t, observer.QueryRow(ctx, `SELECT count(*) FROM pg_locks
	  WHERE pid=$1 AND locktype IN ('relation','advisory')`, conn.PgConn().PID()).Scan(&locks))
	if locks != 0 {
		t.Fatalf("0011 kept %d relation or advisory locks after its timeout", locks)
	}
	// While the long transaction is still open.
	var debtRelations, version int
	must0010(t, conn.QueryRow(ctx, `SELECT
	  (SELECT count(*) FROM pg_class WHERE relnamespace='agentrt'::regnamespace AND relname ~ '^(debt|ratchet|accepted_gap)'),
	  (SELECT max(schema_migration_id) FROM agentrt.schema_migration)`).Scan(&debtRelations, &version))
	if debtRelations != 0 || version != 10 {
		t.Fatalf("after the timeout: %d debt relations, migration %d", debtRelations, version)
	}
	assertSnapshot0011(ctx, t, conn, before)
	must0010(t, tx.Rollback(ctx))
	// The retry runs the same way: on its own goroutine, ended by run0011.end on any way out.
	retry := start0011(ctx, conn, files)
	defer retry.end(t, cancel, func() {})
	select {
	case result = <-retry.done:
	case <-ctx.Done():
		t.Fatal("0011's retry did not end within the run's budget")
	}
	if result.err != nil || !slices.Equal(result.applied, []int{11}) {
		t.Fatalf("0011 after the lock was released: applied %v, err %v", result.applied, result.err)
	}
	assertApplied0011(ctx, t, conn, files[10])
	t.Logf("%s held: 0011 held %d locks on relations of its own, timed out %v after RunFiles began and rolled back whole; the retry applied it (wait seen: %v)",
		c.table, invisible, elapsed.Round(time.Millisecond), seen)
	return seen || c.table == "event"
}

// What RunFiles returned.
type run0011Result struct {
	applied []int
	err     error
}

// A migration run on its own goroutine, so that the test can bound it: done
// carries what RunFiles returned; finished is closed once it has returned.
type run0011 struct {
	conn     *pgx.Conn
	done     chan run0011Result
	finished chan struct{}
}

func start0011(ctx context.Context, conn *pgx.Conn, files []migrate.File) *run0011 {
	r := &run0011{conn: conn, done: make(chan run0011Result, 1), finished: make(chan struct{})}
	go func() {
		defer close(r.finished)
		applied, err := migrate.RunFiles(ctx, conn, "dev", files)
		r.done <- run0011Result{applied, err}
	}()
	return r
}

// end returns only once the run has returned, so that its connection is never
// used or closed by anyone else while the run may still use it (t.Cleanup
// closes it; pgx: a Conn is not for concurrent use). It cancels the run and
// waits up to 5 s; then calls release (which frees a lock the run may wait on,
// itself bounded) and waits up to 5 s more; then closes the connection's
// network socket (net.Conn: Close may be called concurrently and unblocks a
// read or write), after which a run blocked in its rollback or advisory unlock
// (they do not heed the cancel: WithoutCancel in migrate.go) returns with an
// error. From there it waits without a bound of its own: the hard bound of a
// run that never returns is the test binary's (go test -timeout ends the
// process), never a connection handed to cleanup while still in use. A run
// that returned over 5 s after its socket closed is reported.
func (r *run0011) end(t *testing.T, cancel context.CancelFunc, release func()) {
	t.Helper()
	cancel()
	for _, step := range []func(){func() {}, release, func() { _ = r.conn.PgConn().Conn().Close() }} {
		step()
		select {
		case <-r.finished:
			return
		case <-time.After(5 * time.Second):
		}
	}
	<-r.finished
	t.Error("0011's run returned over 5 s after its socket was closed")
}

func lockTimeout(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "55P03"
}

// pinned0010 adds a task pinned to the coordinator (0010's executor_pin) to an
// upgraded 0010 database, made as 0010 allows: by its task_proposed event. A
// snapshot of the task rows then holds a pin to keep.
func pinned0010(t *testing.T, db *agentrttest.DB, conn *pgx.Conn) {
	t.Helper()
	ctx := context.Background()
	rt := db.Connect(t, db.Runtime)
	var spec string
	must0010(t, rt.QueryRow(ctx, `SELECT spec_artifact_id FROM agentrt.task WHERE title='proposed'`).Scan(&spec))
	const pin = "external:coordinator"
	id := store.NewTaskID()
	must0010(t, store.InTx(ctx, rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-1"), Type: "task_proposed", Actor: "controller",
			MissionID: "M-1", TaskID: id, Body: map[string]any{"title": "pinned", "role": "implement", "executor_pin": pin}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO agentrt.task (task_id, mission_id, off_plan, off_plan_reason, role, task_class, title,
		  spec_artifact_id, effect_class, state, hard_limit_s, created_by_actor_id, executor_pin)
		  VALUES ($1, 'M-1', true, '0011 fixture', 'implement', 'go', 'pinned', $2, 'branch', 'proposed', 3600, 'controller', $3)`,
			id, spec, pin)
		return err
	}))
	var n int
	must0010(t, conn.QueryRow(ctx, `SELECT count(*) FROM agentrt.task WHERE executor_pin = $1`, pin).Scan(&n))
	if n != 1 {
		t.Fatalf("%d pinned tasks in the fixture, want 1", n)
	}
}

// What a 0011 that did not apply must leave as it found, as far as these
// read it: schema0010's definitions (constraints, columns, non-internal
// triggers, indexes, functions with their ACLs, migration records); the ACLs
// of the schema, its relations and their columns, the database's default
// privileges and every trigger's enabled state (catalog0011); and the rows of
// every ordinary table and the state of every sequence (data0011). Type ACLs,
// row-level security policies and other catalog attributes are not read.
type snap0011 struct {
	schema, catalog string
	data            map[string]string
}

func snapshot0011(ctx context.Context, t *testing.T, conn *pgx.Conn) snap0011 {
	t.Helper()
	return snap0011{schema: schema0010At(ctx, t, conn), catalog: catalog0011(ctx, t, conn), data: data0011(ctx, t, conn)}
}

func assertSnapshot0011(ctx context.Context, t *testing.T, conn *pgx.Conn, before snap0011) {
	t.Helper()
	after := snapshot0011(ctx, t, conn)
	if after.schema != before.schema {
		t.Fatalf("0011 left the schema changed\nbefore: %s\nafter: %s", before.schema, after.schema)
	}
	if after.catalog != before.catalog {
		t.Fatalf("0011 left ACLs or trigger states changed\nbefore: %s\nafter: %s", before.catalog, after.catalog)
	}
	if !reflect.DeepEqual(after.data, before.data) {
		for name, rows := range before.data {
			if after.data[name] != rows {
				t.Errorf("0011 left rows of %s changed", name)
			}
		}
		for name := range after.data {
			if _, ok := before.data[name]; !ok {
				t.Errorf("0011 left relation %s behind", name)
			}
		}
		t.FailNow()
	}
}

// The ACLs schema0010 does not hold: the schema's own, every relation's
// (tables, sequences, views, indexes), every column's, and the default
// privileges of this database; and every trigger's enabled state, internal
// ones included (a foreign key to an old table adds internal triggers to it).
func catalog0011(ctx context.Context, t *testing.T, conn *pgx.Conn) string {
	t.Helper()
	var out string
	must0010(t, conn.QueryRow(ctx, `SELECT jsonb_build_object(
	  'schema', (SELECT nspacl::text FROM pg_namespace WHERE nspname = 'agentrt'),
	  'relations', (SELECT jsonb_agg(jsonb_build_array(relname, relkind::text, relacl::text) ORDER BY relname)
	    FROM pg_class WHERE relnamespace = 'agentrt'::regnamespace),
	  'columns', (SELECT jsonb_agg(jsonb_build_array(c.relname, a.attname, a.attacl::text) ORDER BY c.relname, a.attname)
	    FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid
	    WHERE c.relnamespace = 'agentrt'::regnamespace AND a.attnum > 0 AND NOT a.attisdropped AND a.attacl IS NOT NULL),
	  'defaults', (SELECT jsonb_agg(jsonb_build_array(defaclrole::regrole::text, defaclnamespace::regnamespace::text, defaclobjtype::text, defaclacl::text)
	    ORDER BY defaclrole::regrole::text, defaclnamespace::regnamespace::text, defaclobjtype) FROM pg_default_acl),
	  'triggers', (SELECT jsonb_agg(jsonb_build_array(c.relname, t.tgname, t.tgenabled::text) ORDER BY c.relname, t.tgname)
	    FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid WHERE c.relnamespace = 'agentrt'::regnamespace)
	)::text`).Scan(&out))
	return out
}

// data0010 with task's every column: it drops executor_pin because it compared
// 0009 (no such column) with 0010; from 0010 on the pin is a row's own data.
func data0011(ctx context.Context, t *testing.T, conn *pgx.Conn) map[string]string {
	t.Helper()
	out := data0010At(ctx, t, conn)
	var tasks string
	must0010(t, conn.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(j ORDER BY j::text), '[]')::text
	  FROM (SELECT to_jsonb(r) AS j FROM agentrt.task r) s`).Scan(&tasks))
	out["task"] = tasks
	return out
}

// 0011 applied: its record is the last, with this file's hash (not
// migrate.Check, which reads every embedded file and so turns behind once a
// 0012 exists), and every debt table it creates is there by name.
func assertApplied0011(ctx context.Context, t *testing.T, conn *pgx.Conn, f migrate.File) {
	t.Helper()
	var top int
	var hash string
	must0010(t, conn.QueryRow(ctx, `SELECT max(schema_migration_id),
	  (SELECT sha256 FROM agentrt.schema_migration WHERE schema_migration_id = 11) FROM agentrt.schema_migration`).Scan(&top, &hash))
	if top != 11 || hash != f.SHA256 {
		t.Fatalf("after 0011: last migration %d, its hash %s, want 11 and %s", top, hash, f.SHA256)
	}
	for _, tbl := range append(append(slices.Clone(debtState), debtAppend...), "debt_transition") {
		var there bool
		must0010(t, conn.QueryRow(ctx, `SELECT to_regclass('agentrt.' || $1) IS NOT NULL`, tbl).Scan(&there))
		if !there {
			t.Errorf("after 0011: no table agentrt.%s", tbl)
		}
	}
}
