package migrate_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
)

// database0022 is a database migrated through 0022 with a scratch table the
// migration owner made, its owner's connection, and every migration file.
func database0022(t *testing.T) (*agentrttest.DB, *pgx.Conn, []migrate.File) {
	t.Helper()
	ctx := context.Background()
	db := agentrttest.New(t)
	conn := db.Connect(t, db.Owner)
	files, err := migrate.Files()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 22 || files[21].Name != "0022_reversible_helpers.sql" {
		t.Fatal("0022 is missing")
	}
	if _, err = migrate.RunFiles(ctx, conn, "dev", files[:22]); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, `CREATE TABLE agentrt.scratch (id int, tag text)`); err != nil {
		t.Fatal(err)
	}
	return db, conn, files
}

func tableExists(t *testing.T, conn *pgx.Conn, name string) bool {
	t.Helper()
	var there bool
	if err := conn.QueryRow(context.Background(), `SELECT to_regclass($1) IS NOT NULL`, name).Scan(&there); err != nil {
		t.Fatal(err)
	}
	return there
}

// Owner 2026-11-15: a migration that cannot destroy rows reaches PROD without
// a card; the platform runs it (cards #493 #554 #558 were Owner-run Jobs).
// 0022 is itself of the class it serves: two CREATE FUNCTION statements. It
// applies on a database at 0021 and adds the two functions and nothing else.
func TestUpgradeTo0022AddsTheTwoHelpers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := agentrttest.New(t)
	conn := db.Connect(t, db.Owner)
	files, err := migrate.Files()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 22 || files[21].ID != 22 {
		t.Fatal("0022 is missing")
	}
	if ok, why := releasepolicy.ClassifyMigration(files[21].SQL); !ok {
		t.Fatalf("0022 is not in the class it serves: %s", why)
	}
	if _, err = migrate.RunFiles(ctx, conn, "dev", files[:21]); err != nil {
		t.Fatal(err)
	}
	shape := func() (functions, tables int) {
		if err := conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM pg_proc WHERE pronamespace = 'agentrt'::regnamespace),
		  (SELECT count(*) FROM pg_tables WHERE schemaname = 'agentrt')`).Scan(&functions, &tables); err != nil {
			t.Fatal(err)
		}
		return
	}
	f0, t0 := shape()
	applied, err := migrate.RunFiles(ctx, conn, "dev", files[:22])
	if err != nil || !slices.Equal(applied, []int{22}) {
		t.Fatalf("0022: applied %v, %v", applied, err)
	}
	if f1, t1 := shape(); f1 != f0+2 || t1 != t0 {
		t.Fatalf("after 0022: %d functions (%d before), %d tables (%d before)", f1, f0, t1, t0)
	}
	var definers int
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM pg_proc WHERE pronamespace = 'agentrt'::regnamespace
	  AND proname IN ('drop_table_if_empty', 'assert_no_rows') AND NOT prosecdef`).Scan(&definers); err != nil || definers != 2 {
		t.Fatalf("the two helpers as SECURITY INVOKER functions: %d, %v", definers, err)
	}
}

// Owner 2026-11-15: a migration that cannot destroy rows reaches PROD without
// a card; the platform runs it (cards #493 #554 #558 were Owner-run Jobs).
// drop_table_if_empty drops an empty table; with one row, or without the
// table, it raises naming the table and drops nothing.
func TestDropTableIfEmpty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, conn, _ := database0022(t)
	drop := func(c *pgx.Conn, name string) error {
		_, err := c.Exec(ctx, `SELECT agentrt.drop_table_if_empty($1)`, name)
		return err
	}
	if _, err := conn.Exec(ctx, `INSERT INTO agentrt.scratch VALUES (1, 'a')`); err != nil {
		t.Fatal(err)
	}
	if err := drop(conn, "agentrt.scratch"); err == nil || !strings.Contains(err.Error(), "table agentrt.scratch has at least one row; nothing is dropped") {
		t.Fatalf("one row: %v", err)
	}
	if !tableExists(t, conn, "agentrt.scratch") {
		t.Fatal("a table with a row was dropped")
	}
	for name, want := range map[string]string{
		"agentrt.no_such_table": "table agentrt.no_such_table does not exist; nothing is dropped",
		"scratch":               "scratch is not a schema-qualified table name",
		"db.agentrt.scratch":    "is not a schema-qualified table name",
		// Not a name: the text never reaches a statement.
		"agentrt.scratch; DROP TABLE agentrt.event": "",
		"agentrt.scratch CASCADE":                   "",
		"":                                          "",
	} {
		if err := drop(conn, name); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%q: %v, want an error with %q", name, err, want)
		}
	}
	if !tableExists(t, conn, "agentrt.scratch") || !tableExists(t, conn, "agentrt.event") {
		t.Fatal("a refused call dropped a table")
	}
	// The runtime role does not own the table: the function gives it nothing.
	if _, err := conn.Exec(ctx, `DELETE FROM agentrt.scratch`); err != nil {
		t.Fatal(err)
	}
	if err := drop(db.Connect(t, db.Runtime), "agentrt.scratch"); err == nil {
		t.Fatal("the runtime role dropped a table through the helper")
	}
	if !tableExists(t, conn, "agentrt.scratch") {
		t.Fatal("the runtime role's call dropped the table")
	}
	// Something that depends on the table: no CASCADE, so an error.
	if _, err := conn.Exec(ctx, `CREATE VIEW agentrt.scratch_view AS SELECT id FROM agentrt.scratch`); err != nil {
		t.Fatal(err)
	}
	if err := drop(conn, "agentrt.scratch"); err == nil || !tableExists(t, conn, "agentrt.scratch") {
		t.Fatalf("a table a view depends on: %v", err)
	}
	if _, err := conn.Exec(ctx, `DROP VIEW agentrt.scratch_view`); err != nil {
		t.Fatal(err)
	}
	if err := drop(conn, "agentrt.scratch"); err != nil || tableExists(t, conn, "agentrt.scratch") {
		t.Fatalf("an empty table: %v", err)
	}
	// A name with odd characters is one identifier, quoted as one.
	const odd = `"odd ""; DROP TABLE agentrt.event; --"`
	if _, err := conn.Exec(ctx, `CREATE TABLE agentrt.`+odd+` (id int)`); err != nil {
		t.Fatal(err)
	}
	if err := drop(conn, "agentrt."+odd); err != nil || tableExists(t, conn, "agentrt."+odd) || !tableExists(t, conn, "agentrt.event") {
		t.Fatalf("an oddly named empty table: %v", err)
	}
}

// Owner 2026-11-15: a migration that cannot destroy rows reaches PROD without
// a card; the platform runs it (cards #493 #554 #558 were Owner-run Jobs).
// assert_no_rows passes when no row matches and raises naming the table, the
// column and the pattern when one does; a column or a pattern with odd
// characters is quoted or passed as a parameter, never run.
func TestAssertNoRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, conn, _ := database0022(t)
	check := func(tbl, col, pattern string) error {
		_, err := conn.Exec(ctx, `SELECT agentrt.assert_no_rows($1, $2, $3)`, tbl, col, pattern)
		return err
	}
	if _, err := conn.Exec(ctx, `INSERT INTO agentrt.scratch VALUES (1, 'debt:TD-1'), (2, 'note')`); err != nil {
		t.Fatal(err)
	}
	if err := check("agentrt.scratch", "tag", "gap:%"); err != nil {
		t.Fatalf("no matching row: %v", err)
	}
	if err := check("agentrt.scratch", "id", "9%"); err != nil {
		t.Fatalf("a column that is not text: %v", err)
	}
	err := check("agentrt.scratch", "tag", "debt:%")
	if err == nil || !strings.Contains(err.Error(), "table agentrt.scratch has at least one row whose column tag is LIKE 'debt:%'") {
		t.Fatalf("a matching row: %v", err)
	}
	for name, c := range map[string]struct{ tbl, col, pattern, want string }{
		"a missing table":        {"agentrt.no_such_table", "tag", "%", "table agentrt.no_such_table does not exist"},
		"an unqualified table":   {"scratch", "tag", "%", "is not a schema-qualified table name"},
		"a missing column":       {"agentrt.scratch", "no_such_column", "%", "no_such_column"},
		"a column that is SQL":   {"agentrt.scratch", `tag LIKE '%' OR true; DROP TABLE agentrt.scratch; --`, "zzz", "does not exist"},
		"a table that is SQL":    {"agentrt.scratch WHERE false; DROP TABLE agentrt.scratch; --", "tag", "%", ""},
		"a pattern with a quote": {"agentrt.scratch", "tag", "debt:%' OR 'a' = 'a", ""},
	} {
		err := check(c.tbl, c.col, c.pattern)
		if name == "a pattern with a quote" {
			// The pattern is only a pattern: it matches no row, so no error.
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: %v, want an error with %q", name, err, c.want)
		}
	}
	var rows int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM agentrt.scratch`).Scan(&rows); err != nil || rows != 2 {
		t.Fatalf("the table after every call: %d rows, %v", rows, err)
	}
}

// Owner 2026-11-15: a migration that cannot destroy rows reaches PROD without
// a card; the platform runs it (cards #493 #554 #558 were Owner-run Jobs).
// A migration that calls a helper runs in one transaction: when the helper
// raises, what the file did before it is rolled back, no record of the
// migration is written, and the error names the table. With the row gone
// the same file applies and the table is dropped.
func TestAMigrationThatCallsAHelperRollsBackWhole(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, conn, files := database0022(t)
	sql := "CREATE TABLE agentrt.made_first (id int);\n" +
		"SELECT agentrt.assert_no_rows('agentrt.scratch', 'tag', 'debt:%');\n" +
		"SELECT agentrt.drop_table_if_empty('agentrt.scratch');\n"
	if ok, why := releasepolicy.ClassifyMigration(sql); !ok {
		t.Fatalf("the migration under test is not in the class: %s", why)
	}
	next := append(slices.Clone(files[:22]), migrate.File{ID: 23, Name: "0023_drop_scratch.sql", SQL: sql, SHA256: strings.Repeat("a", 64)})
	record := func() (n int) {
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM agentrt.schema_migration WHERE schema_migration_id = 23`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, c := range []struct{ put, want string }{
		{`INSERT INTO agentrt.scratch VALUES (1, 'debt:TD-1')`, "table agentrt.scratch has at least one row whose column tag is LIKE 'debt:%'"},
		{`UPDATE agentrt.scratch SET tag = 'note'`, "table agentrt.scratch has at least one row; nothing is dropped"},
	} {
		if _, err := conn.Exec(ctx, c.put); err != nil {
			t.Fatal(err)
		}
		applied, err := migrate.RunFiles(ctx, conn, "dev", next)
		if err == nil || !strings.Contains(err.Error(), c.want) || len(applied) != 0 {
			t.Fatalf("applied %v, %v, want an error with %q", applied, err, c.want)
		}
		if !tableExists(t, conn, "agentrt.scratch") || tableExists(t, conn, "agentrt.made_first") || record() != 0 {
			t.Fatalf("after the refusal: scratch there %v, made_first there %v, %d record of 0023",
				tableExists(t, conn, "agentrt.scratch"), tableExists(t, conn, "agentrt.made_first"), record())
		}
	}
	if _, err := conn.Exec(ctx, `DELETE FROM agentrt.scratch`); err != nil {
		t.Fatal(err)
	}
	applied, err := migrate.RunFiles(ctx, conn, "dev", next)
	if err != nil || !slices.Equal(applied, []int{23}) || tableExists(t, conn, "agentrt.scratch") || !tableExists(t, conn, "agentrt.made_first") {
		t.Fatalf("on an empty table: applied %v, %v", applied, err)
	}
}
