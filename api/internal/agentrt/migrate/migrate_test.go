package migrate_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func TestFilesAreNumberedWithoutGaps(t *testing.T) {
	files, err := migrate.Files()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 3 {
		t.Fatalf("found %d migrations", len(files))
	}
}

// T19: a second run changes nothing; two migrators started together apply
// each file once; an applied file edited by one byte stops the run.
func TestRunIsIdempotentSerialAndRefusesEditedFiles(t *testing.T) {
	db := agentrttest.New(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	results := make([][]int, 2)
	errs := make([]error, 2)
	for i := range 2 {
		c := db.Connect(t, db.Owner)
		wg.Add(1)
		go func(i int, c *pgx.Conn) {
			defer wg.Done()
			results[i], errs[i] = migrate.Run(ctx, c, "dev")
		}(i, c)
	}
	wg.Wait()
	files, _ := migrate.Files()
	total := 0
	for i := range 2 {
		if errs[i] != nil {
			t.Fatalf("migrator %d: %v", i, errs[i])
		}
		total += len(results[i])
	}
	if total != len(files) {
		t.Fatalf("the two migrators applied %d files together, want %d (each once)", total, len(files))
	}

	again, err := migrate.Run(ctx, db.Connect(t, db.Owner), "dev")
	if err != nil || len(again) != 0 {
		t.Fatalf("second run applied %v, err %v", again, err)
	}

	edited := append([]migrate.File(nil), files...)
	edited[0].SQL += " "
	edited[0].SHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	if _, err := migrate.RunFiles(ctx, db.Connect(t, db.Owner), "dev", edited); !errors.Is(err, migrate.ErrEdited) {
		t.Fatalf("edited file: err = %v, want ErrEdited", err)
	}
}

func TestUnknownEnvironmentIsRefused(t *testing.T) {
	if _, err := migrate.RuntimeRole("qa"); err == nil {
		t.Fatal("qa accepted")
	}
}

// 0005 on a database that already has events (review 166 B2): the events
// stay as they were, the vocabulary gains attempt_cancelled and keeps every
// type it had.
func TestUpgradeTo0005KeepsEvents(t *testing.T) {
	ctx := context.Background()
	db := agentrttest.New(t)
	conn := db.Connect(t, db.Owner)
	files, err := migrate.Files()
	if err != nil || len(files) < 5 || files[4].ID != 5 {
		t.Fatalf("files = %d %v", len(files), err)
	}
	if _, err = migrate.RunFiles(ctx, conn, "dev", files[:4]); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Seed(ctx, conn); err != nil {
		t.Fatal(err)
	}
	// Every event, every column, in stream order (review 169 B2).
	var before, after int
	var rowsBefore, rowsAfter string
	rows := `SELECT count(*), COALESCE(string_agg(to_jsonb(e)::text, E'\n' ORDER BY event_stream_id, seq), '') FROM agentrt.event e`
	if err = conn.QueryRow(ctx, rows).Scan(&before, &rowsBefore); err != nil || before == 0 {
		t.Fatalf("events before = %d %v", before, err)
	}
	var defBefore string
	if err = conn.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'event_type_closed'`).Scan(&defBefore); err != nil {
		t.Fatal(err)
	}
	applied, err := migrate.RunFiles(ctx, conn, "dev", files)
	if err != nil || len(applied) != len(files)-4 || applied[0] != 5 {
		t.Fatalf("applied %v %v", applied, err)
	}
	if err = conn.QueryRow(ctx, rows).Scan(&after, &rowsAfter); err != nil || after != before || rowsAfter != rowsBefore {
		t.Fatalf("events after = %d (was %d) %v; changed: %v", after, before, err, rowsAfter != rowsBefore)
	}
	var def string
	if err = conn.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'event_type_closed'`).Scan(&def); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(def, "'attempt_cancelled'") || strings.Contains(defBefore, "'attempt_cancelled'") {
		t.Fatalf("constraint before %q, after %q", defBefore, def)
	}
	for _, typ := range strings.Split(strings.TrimSpace(defBefore[strings.Index(defBefore, "ARRAY[")+6:strings.LastIndex(defBefore, "]")]), ",") {
		if !strings.Contains(def, strings.TrimSpace(typ)) {
			t.Fatalf("0005 dropped %s", typ)
		}
	}
	// The constraint itself, on a table of its own: the new type and an old
	// one are taken, a type outside the vocabulary is refused (review 168 B2).
	if _, err = conn.Exec(ctx, `CREATE TEMP TABLE vocab (event_type text, CONSTRAINT event_type_closed `+def+`)`); err != nil {
		t.Fatal(err)
	}
	for typ, want := range map[string]bool{"attempt_cancelled": true, "attempt_salvaged": true, "attempt_vanished": false} {
		_, err = conn.Exec(ctx, `INSERT INTO vocab VALUES ($1)`, typ)
		if (err == nil) != want {
			t.Errorf("%s taken = %v (%v)", typ, err == nil, err)
		}
	}
	if again, rerr := migrate.RunFiles(ctx, conn, "dev", files); rerr != nil || len(again) != 0 {
		t.Fatalf("a second run applied %v %v", again, rerr)
	}
	if err = conn.QueryRow(ctx, rows).Scan(&after, &rowsAfter); err != nil || after != before || rowsAfter != rowsBefore {
		t.Fatalf("events after a second run = %d (was %d) %v; changed: %v", after, before, err, rowsAfter != rowsBefore)
	}
}

// 0006 on a database that already has events: they stay as they were, the
// vocabulary gains decision_recorded and keeps every type it had (W-31 2.2b,
// the Owner's D2 A).
func TestUpgradeTo0006KeepsEvents(t *testing.T) {
	ctx := context.Background()
	db := agentrttest.New(t)
	conn := db.Connect(t, db.Owner)
	files, err := migrate.Files()
	if err != nil || len(files) < 6 || files[5].ID != 6 {
		t.Fatalf("files = %d %v", len(files), err)
	}
	if _, err = migrate.RunFiles(ctx, conn, "dev", files[:5]); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Seed(ctx, conn); err != nil {
		t.Fatal(err)
	}
	var before, after int
	var rowsBefore, rowsAfter string
	rows := `SELECT count(*), COALESCE(string_agg(to_jsonb(e)::text, E'\n' ORDER BY event_stream_id, seq), '') FROM agentrt.event e`
	if err = conn.QueryRow(ctx, rows).Scan(&before, &rowsBefore); err != nil || before == 0 {
		t.Fatalf("events before = %d %v", before, err)
	}
	constraint := `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'event_type_closed'`
	var defBefore, def string
	if err = conn.QueryRow(ctx, constraint).Scan(&defBefore); err != nil {
		t.Fatal(err)
	}
	applied, err := migrate.RunFiles(ctx, conn, "dev", files)
	if err != nil || len(applied) != len(files)-5 || applied[0] != 6 {
		t.Fatalf("applied %v %v", applied, err)
	}
	if err = conn.QueryRow(ctx, rows).Scan(&after, &rowsAfter); err != nil || after != before || rowsAfter != rowsBefore {
		t.Fatalf("events after = %d (was %d) %v; changed: %v", after, before, err, rowsAfter != rowsBefore)
	}
	if err = conn.QueryRow(ctx, constraint).Scan(&def); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(def, "'decision_recorded'") || strings.Contains(defBefore, "'decision_recorded'") {
		t.Fatalf("constraint before %q, after %q", defBefore, def)
	}
	for _, typ := range strings.Split(strings.TrimSpace(defBefore[strings.Index(defBefore, "ARRAY[")+6:strings.LastIndex(defBefore, "]")]), ",") {
		if !strings.Contains(def, strings.TrimSpace(typ)) {
			t.Fatalf("0006 dropped %s", typ)
		}
	}
	if again, rerr := migrate.RunFiles(ctx, conn, "dev", files); rerr != nil || len(again) != 0 {
		t.Fatalf("a second run applied %v %v", again, rerr)
	}
}

// TD-397: 0007 replaces the CHECK that ties verify and review to a subject
// with one that also takes a gate task (role verify, no subject, class
// gate, effect none, off the plan, made by the controller), and changes no
// other CHECK; a second run applies nothing. Which tasks the new CHECK
// takes is store's TestGateTaskConstraint; an older platform on the new
// schema is not run here.
func TestUpgradeTo0007ReplacesTheSubjectCheck(t *testing.T) {
	ctx := context.Background()
	db := agentrttest.New(t)
	conn := db.Connect(t, db.Owner)
	files, err := migrate.Files()
	if err != nil || len(files) < 7 || files[6].ID != 7 {
		t.Fatalf("files = %d %v", len(files), err)
	}
	if _, err = migrate.RunFiles(ctx, conn, "dev", files[:6]); err != nil {
		t.Fatal(err)
	}
	checks := `SELECT string_agg(conname || '=' || pg_get_constraintdef(oid), E'\n' ORDER BY conname) FROM pg_constraint
	  WHERE conrelid = 'agentrt.task'::regclass AND contype = 'c'`
	var before, after string
	if err = conn.QueryRow(ctx, checks).Scan(&before); err != nil {
		t.Fatal(err)
	}
	// Through 0007 only: a later file may add a CHECK of its own (0010
	// adds task_executor_pin_check).
	applied, err := migrate.RunFiles(ctx, conn, "dev", files[:7])
	if err != nil || len(applied) != 1 || applied[0] != 7 {
		t.Fatalf("applied %v %v", applied, err)
	}
	if err = conn.QueryRow(ctx, checks).Scan(&after); err != nil {
		t.Fatal(err)
	}
	old := "task_check2=CHECK (((role = ANY (ARRAY['verify'::text, 'review'::text])) = (subject_task_id IS NOT NULL)))"
	if !strings.Contains(before, old) || strings.Contains(after, old) || !strings.Contains(after, "task_subject_by_role=CHECK") ||
		!strings.Contains(after, "task_class = 'gate'::text") || !strings.Contains(after, "created_by_actor_id = 'controller'::text") {
		t.Fatalf("before:\n%s\nafter:\n%s", before, after)
	}
	same := func(defs string) string {
		var keep []string
		for _, l := range strings.Split(defs, "\n") {
			if !strings.HasPrefix(l, "task_check2=") && !strings.HasPrefix(l, "task_subject_by_role=") {
				keep = append(keep, l)
			}
		}
		return strings.Join(keep, "\n")
	}
	if same(before) != same(after) {
		t.Fatalf("0007 changed another CHECK:\n%s\n---\n%s", same(before), same(after))
	}
	if again, rerr := migrate.RunFiles(ctx, conn, "dev", files[:7]); rerr != nil || len(again) != 0 {
		t.Fatalf("a second run applied %v %v", again, rerr)
	}
}

// T19, the three environments (review 145 B7): a database migrated for an
// environment grants its own runtime role, platform_agentrt_<env>, the same
// privileges on every table of the schema whichever the environment, and the
// other two environments' runtime roles nothing there: no table privilege,
// no use of the schema; connected as one of them, a read is refused. No
// runtime role may update or delete an event (review 334 B1: the whole
// matrix, and a real connection).
func TestEachEnvironmentGrantsItsOwnRuntimeOnly(t *testing.T) {
	ctx := context.Background()
	envs := []string{"dev", "stg", "prod"}
	setup := agentrttest.New(t)
	admin := setup.Connect(t, setup.Admin)
	for _, env := range envs {
		role, err := migrate.RuntimeRole(env)
		if err != nil {
			t.Fatal(err)
		}
		// Roles are the cluster's: the test cluster may not have the STG and
		// PROD ones yet.
		if _, err = admin.Exec(ctx, `DO $$ BEGIN EXECUTE format('CREATE ROLE %I LOGIN', '`+role+`');
		  EXCEPTION WHEN duplicate_object OR unique_violation THEN NULL; END $$`); err != nil {
			t.Fatal(err)
		}
	}
	// privileges is every table privilege role has in the schema, and each
	// column it may update where it may not update the whole table, as
	// "table PRIV" and "table UPDATE(column)" lines (review 335 B2).
	privileges := func(conn *pgx.Conn, role string) []string {
		t.Helper()
		rows, err := conn.Query(ctx, `SELECT c.relname || ' ' || p.priv FROM pg_class c
		  CROSS JOIN unnest(ARRAY['SELECT','INSERT','UPDATE','DELETE','TRUNCATE','REFERENCES','TRIGGER']) p(priv)
		  WHERE c.relnamespace = 'agentrt'::regnamespace AND c.relkind IN ('r', 'p') AND has_table_privilege($1, c.oid, p.priv)
		  UNION ALL SELECT c.relname || ' UPDATE(' || a.attname || ')' FROM pg_class c JOIN pg_attribute a ON a.attrelid = c.oid
		  WHERE c.relnamespace = 'agentrt'::regnamespace AND c.relkind IN ('r', 'p') AND a.attnum > 0 AND NOT a.attisdropped
		    AND NOT has_table_privilege($1, c.oid, 'UPDATE') AND has_column_privilege($1, c.oid, a.attnum, 'UPDATE')
		  ORDER BY 1`, role)
		if err != nil {
			t.Fatal(err)
		}
		out, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	var own [][]string
	for _, env := range envs {
		db := agentrttest.New(t)
		if _, err := migrate.Run(ctx, db.Connect(t, db.Owner), env); err != nil {
			t.Fatalf("%s: %v", env, err)
		}
		check := db.Connect(t, db.Admin)
		for _, other := range envs {
			role, _ := migrate.RuntimeRole(other)
			got := privileges(check, role)
			var usage, update, del bool
			if err := check.QueryRow(ctx, `SELECT has_schema_privilege($1, 'agentrt', 'USAGE'),
			    has_table_privilege($1, 'agentrt.event', 'UPDATE'), has_table_privilege($1, 'agentrt.event', 'DELETE')`, role).Scan(&usage, &update, &del); err != nil {
				t.Fatal(err)
			}
			switch {
			case other == env && (!usage || len(got) == 0 || update || del):
				t.Errorf("%s database, its own %s: usage %v, event update %v, event delete %v, privileges %v", env, role, usage, update, del, got)
			case other != env && (usage || len(got) != 0):
				t.Errorf("%s database, %s of another environment: usage %v, privileges %v", env, role, usage, got)
			}
			if other == env {
				own = append(own, got)
			}
		}
		// Connected as another environment's runtime role (given CONNECT
		// here, as a misconfigured cluster might), a read is refused.
		stranger, _ := migrate.RuntimeRole(map[string]string{"dev": "stg", "stg": "prod", "prod": "dev"}[env])
		if _, err := check.Exec(ctx, `GRANT CONNECT ON DATABASE `+pgx.Identifier{db.Name}.Sanitize()+` TO `+pgx.Identifier{stranger}.Sanitize()); err != nil {
			t.Fatal(err)
		}
		conn := db.Connect(t, db.Admin+" user="+stranger)
		var n int
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM agentrt.event`).Scan(&n); err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("%s database read as %s: %v", env, stranger, err)
		}
	}
	for i := 1; i < len(own); i++ {
		if !slices.Equal(own[i], own[0]) {
			t.Errorf("%s's runtime has %v; dev's has %v", envs[i], own[i], own[0])
		}
	}
}
