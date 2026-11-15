package releasepolicy_test

import (
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
)

// Owner 2026-11-15: a migration that cannot destroy rows reaches PROD without
// a card; the platform runs it (cards #493 #554 #558 were Owner-run Jobs).
// Every statement kind of the class, alone and together in one file.
func TestClassifyMigrationInClass(t *testing.T) {
	in := map[string]string{
		"create table":             "CREATE TABLE agentrt.note (note_id bigint PRIMARY KEY, body text NOT NULL);",
		"create table if absent":   "create table if not exists note (id bigint)",
		"generated column":         "CREATE TABLE t (a int, b int GENERATED ALWAYS AS (a * 2) STORED);",
		"create schema":            "CREATE SCHEMA IF NOT EXISTS reports;",
		"create index":             "CREATE INDEX note_body ON agentrt.note (body);",
		"create unique index":      "CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS note_u ON note (lower(body)) WHERE body <> '';",
		"create type":              "CREATE TYPE mood AS ENUM ('a', 'b');",
		"create view":              "CREATE VIEW v AS SELECT 1;",
		"create or replace view":   "CREATE OR REPLACE VIEW v AS SELECT note_id FROM agentrt.note;",
		"function, body opaque":    "CREATE OR REPLACE FUNCTION f() RETURNS void LANGUAGE plpgsql AS $fn$ BEGIN DELETE FROM x; DROP TABLE y CASCADE; END $fn$;",
		"function, string body":    "CREATE FUNCTION g() RETURNS int LANGUAGE sql AS 'SELECT 1; TRUNCATE x';",
		"create trigger":           "CREATE TRIGGER note_guard BEFORE UPDATE ON agentrt.note FOR EACH ROW EXECUTE FUNCTION f();",
		"drop function":            "DROP FUNCTION agentrt.old_fn(text, jsonb);",
		"drop trigger":             "DROP TRIGGER IF EXISTS note_guard ON agentrt.note;",
		"drop view":                "DROP VIEW v RESTRICT;",
		"drop index":               "DROP INDEX CONCURRENTLY IF EXISTS note_body;",
		"add nullable column":      "ALTER TABLE agentrt.note ADD COLUMN tag text;",
		"add column with default":  "ALTER TABLE ONLY note ADD COLUMN IF NOT EXISTS n int NOT NULL DEFAULT 0;",
		"add column with a check":  "ALTER TABLE note ADD COLUMN m int CHECK (m IS NOT NULL OR tag IS NULL);",
		"add and drop constraint":  "ALTER TABLE agentrt.event DROP CONSTRAINT event_type_closed, ADD CONSTRAINT event_type_closed CHECK (event_type IN ('a', 'b; c'));",
		"several actions":          "ALTER TABLE IF EXISTS \"Odd Name\".note ADD COLUMN a int, ADD COLUMN b numeric(10, 2) DEFAULT 0, DROP CONSTRAINT IF EXISTS c;",
		"comment":                  "COMMENT ON TABLE agentrt.note IS 'notes; DROP TABLE is only a word here';",
		"drop table if empty":      "SELECT agentrt.drop_table_if_empty('agentrt.note');",
		"assert no rows":           "select agentrt.assert_no_rows ( 'agentrt.event', 'event_stream_id', 'debt:%' ) ;",
		"comments are not read":    "-- DROP TABLE x;\n/* TRUNCATE y; /* nested */ DELETE FROM z; */\nCREATE TABLE t (id int); -- CASCADE",
		"a quote inside a string":  "COMMENT ON TABLE t IS 'it''s; fine';",
		"a semicolon in a name":    "CREATE TABLE \"a;b\" (id int);",
		"the helpers, first made":  "CREATE FUNCTION agentrt.drop_table_if_empty(tbl text) RETURNS void LANGUAGE plpgsql AS $$ BEGIN NULL; END $$;",
		"a parameter is no quote":  "CREATE FUNCTION h(int) RETURNS int LANGUAGE sql AS $$ SELECT $1 $$;",
		"empty statements between": ";;CREATE TABLE t (id int);;",
	}
	var all []string
	for name, sql := range in {
		if ok, why := releasepolicy.ClassifyMigration(sql); !ok {
			t.Errorf("%s: not in the class: %s", name, why)
		}
		all = append(all, strings.TrimRight(strings.TrimSpace(sql), ";"))
	}
	if ok, why := releasepolicy.ClassifyMigration(strings.Join(all, ";\n") + ";\n"); !ok {
		t.Errorf("all of them in one file: %s", why)
	}
}

// Owner 2026-11-15: a migration that cannot destroy rows reaches PROD without
// a card; the platform runs it (cards #493 #554 #558 were Owner-run Jobs).
// What is never in the class, each named by the kind that put it out. The
// refusal is of the whole file, whatever else it holds.
func TestClassifyMigrationOutOfClass(t *testing.T) {
	for name, c := range map[string]struct{ sql, why string }{
		"an empty file":                 {"", "holds no statement"},
		"only comments":                 {"-- nothing\n/* at all */\n", "holds no statement"},
		"DO":                            {"DO $$ BEGIN DELETE FROM x; END $$;", "DO is not a statement of the class"},
		"DROP TABLE":                    {"DROP TABLE agentrt.note;", "DROP TABLE is not"},
		"DROP TABLE CASCADE":            {"DROP TABLE agentrt.note CASCADE;", "DROP TABLE with CASCADE"},
		"TRUNCATE":                      {"TRUNCATE agentrt.note;", "TRUNCATE is not"},
		"DELETE":                        {"DELETE FROM agentrt.note WHERE note_id < 10;", "DELETE is not"},
		"UPDATE":                        {"UPDATE agentrt.note SET body = '';", "UPDATE is not"},
		"INSERT":                        {"INSERT INTO agentrt.note VALUES (1, 'x');", "INSERT is not"},
		"DROP COLUMN":                   {"ALTER TABLE agentrt.note DROP COLUMN body;", "ALTER TABLE DROP COLUMN"},
		"DROP column, no keyword":       {"ALTER TABLE agentrt.note DROP body;", "ALTER TABLE DROP"},
		"ALTER COLUMN":                  {"ALTER TABLE note ALTER COLUMN body TYPE int USING 0;", "ALTER TABLE ALTER COLUMN"},
		"RENAME":                        {"ALTER TABLE note RENAME TO note_old;", "ALTER TABLE RENAME"},
		"RENAME COLUMN":                 {"ALTER TABLE note RENAME COLUMN body TO b;", "ALTER TABLE RENAME COLUMN"},
		"an allowed action and another": {"ALTER TABLE note ADD COLUMN a int, DROP COLUMN body;", "ALTER TABLE DROP COLUMN"},
		"ADD without COLUMN":            {"ALTER TABLE note ADD a int;", "ALTER TABLE ADD"},
		"OWNER TO":                      {"ALTER TABLE note OWNER TO someone;", "ALTER TABLE OWNER"},
		"DISABLE TRIGGER":               {"ALTER TABLE note DISABLE TRIGGER ALL;", "ALTER TABLE DISABLE TRIGGER"},
		"NOT NULL without DEFAULT":      {"ALTER TABLE note ADD COLUMN n int NOT NULL;", "NOT NULL without a DEFAULT"},
		"DROP CONSTRAINT CASCADE":       {"ALTER TABLE note DROP CONSTRAINT c CASCADE;", "ALTER TABLE with CASCADE"},
		"ON DELETE CASCADE":             {"ALTER TABLE note ADD CONSTRAINT fk FOREIGN KEY (p) REFERENCES parent (id) ON DELETE CASCADE;", "with CASCADE"},
		"DROP VIEW CASCADE":             {"DROP VIEW v CASCADE;", "DROP VIEW with CASCADE"},
		"DROP FUNCTION CASCADE":         {"DROP FUNCTION f() cascade;", "DROP FUNCTION with CASCADE"},
		"GRANT":                         {"GRANT ALL ON agentrt.note TO PUBLIC;", "GRANT is not"},
		"REVOKE":                        {"REVOKE SELECT ON agentrt.note FROM reader;", "REVOKE is not"},
		"DROP SCHEMA":                   {"DROP SCHEMA agentrt;", "DROP SCHEMA is not"},
		"DROP TYPE":                     {"DROP TYPE mood;", "DROP TYPE is not"},
		"DROP SEQUENCE":                 {"DROP SEQUENCE agentrt.s;", "DROP SEQUENCE is not"},
		"SET ROLE":                      {"SET ROLE postgres;", "SET is not"},
		"SET LOCAL":                     {"SET LOCAL session_replication_role = replica;", "SET is not"},
		"RESET":                         {"RESET ROLE;", "RESET is not"},
		"COPY":                          {"COPY agentrt.note FROM '/tmp/x';", "COPY is not"},
		"CREATE TABLE AS":               {"CREATE TABLE copy AS SELECT * FROM agentrt.note;", "CREATE TABLE AS"},
		"CREATE TABLE AS, after WITH":   {"CREATE TABLE copy (a) WITH (fillfactor = 70) AS TABLE agentrt.note;", "CREATE TABLE AS"},
		"CREATE TEMP TABLE":             {"CREATE TEMP TABLE t (id int);", "CREATE TEMP is not"},
		"CREATE MATERIALIZED VIEW":      {"CREATE MATERIALIZED VIEW m AS SELECT 1;", "CREATE MATERIALIZED is not"},
		"CREATE EXTENSION":              {"CREATE EXTENSION IF NOT EXISTS pgcrypto;", "CREATE EXTENSION is not"},
		"CREATE PROCEDURE":              {"CREATE PROCEDURE p() LANGUAGE sql AS $$ SELECT 1 $$;", "CREATE PROCEDURE is not"},
		"CREATE RULE":                   {"CREATE RULE r AS ON INSERT TO note DO ALSO DELETE FROM note;", "CREATE RULE is not"},
		"CREATE EVENT TRIGGER":          {"CREATE EVENT TRIGGER e ON ddl_command_start EXECUTE FUNCTION f();", "CREATE EVENT is not"},
		"CREATE SCHEMA with elements":   {"CREATE SCHEMA s GRANT ALL ON SCHEMA s TO PUBLIC;", "CREATE SCHEMA with more than a name"},
		"CREATE SCHEMA AUTHORIZATION":   {"CREATE SCHEMA s AUTHORIZATION someone;", "CREATE SCHEMA with more than a name"},
		"ALTER FUNCTION":                {"ALTER FUNCTION f() OWNER TO someone;", "ALTER FUNCTION is not"},
		"SELECT of anything else":       {"SELECT pg_terminate_backend(1);", "SELECT of anything but"},
		"SELECT of two things":          {"SELECT agentrt.drop_table_if_empty('a.b'), lo_unlink(1);", "SELECT of anything but"},
		"helper, not a literal":         {"SELECT agentrt.drop_table_if_empty('a.' || tablename) FROM pg_tables;", "SELECT of anything but"},
		"helper, a dollar argument":     {"SELECT agentrt.drop_table_if_empty($$a.b$$);", "SELECT of anything but"},
		"helper, wrong arity":           {"SELECT agentrt.assert_no_rows('a.b', 'c');", "SELECT of anything but"},
		"helper, another schema":        {"SELECT public.drop_table_if_empty('a.b');", "SELECT of anything but"},
		"helper replaced":               {"CREATE OR REPLACE FUNCTION agentrt.assert_no_rows(a text, b text, c text) RETURNS void LANGUAGE sql AS $$ SELECT 1 $$;", "CREATE OR REPLACE FUNCTION of a helper"},
		"helper dropped":                {"DROP FUNCTION agentrt.\"drop_table_if_empty\"(text);", "DROP FUNCTION of a helper"},
		"BEGIN ATOMIC body":             {"CREATE FUNCTION f() RETURNS void LANGUAGE sql BEGIN ATOMIC DELETE FROM x; END;", "statement 2: END is not"},
		"WITH ... DELETE":               {"WITH gone AS (DELETE FROM x RETURNING 1) SELECT 1;", "WITH is not"},
		"a statement after a good one":  {"CREATE TABLE t (id int);\nDROP TABLE u;", "statement 2: DROP TABLE"},
		"hidden behind a comment":       {"CREATE TABLE t (id int) /* ; */ ; TRUNCATE t", "statement 2: TRUNCATE"},
		"a string closed early":         {"COMMENT ON TABLE t IS 'a'; DELETE FROM t; --'", "statement 2: DELETE"},
		"an unterminated string":        {"COMMENT ON TABLE t IS 'a; CREATE TABLE u (id int);", "a single-quoted string is not closed"},
		"an unterminated identifier":    {"CREATE TABLE \"t (id int);", "a double-quoted identifier is not closed"},
		"an unterminated body":          {"CREATE FUNCTION f() RETURNS void LANGUAGE sql AS $a$ SELECT 1 $b$;", "a dollar-quoted body ($a$) is not closed"},
		"an unterminated comment":       {"CREATE TABLE t (id int); /* DROP", "a /* comment is not closed"},
		"an escape string":              {"COMMENT ON TABLE t IS E'a\\'; DROP TABLE t; --';", "an E'' string"},
		"psql meta-command":             {"\\connect other\nCREATE TABLE t (id int);", "starts with no keyword"},
	} {
		ok, why := releasepolicy.ClassifyMigration(c.sql)
		if ok || !strings.Contains(why, c.why) {
			t.Errorf("%s: ClassifyMigration = %v %q, want out of the class for %q", name, ok, why, c.why)
		}
		// The same statement after one of the class puts the file out too.
		if c.sql != "" && !strings.Contains(name, "only comments") {
			if ok, _ := releasepolicy.ClassifyMigration("CREATE TABLE first_one (id int);\n" + c.sql); ok {
				t.Errorf("%s: in the class once a CREATE TABLE stands before it", name)
			}
		}
	}
}
