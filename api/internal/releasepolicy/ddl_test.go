package releasepolicy_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

func TestClassifyDDL(t *testing.T) {
	base := "CREATE TABLE orders (id bigint PRIMARY KEY);\n"
	reorderedBefore := "CREATE TABLE a (id int);\nALTER TABLE a ADD COLUMN x int;"
	reorderedAfter := "ALTER TABLE a ADD COLUMN x int;\nCREATE TABLE a (id int);"
	for _, tc := range []struct {
		name, before, after string
		ok                  bool
		why                 string
	}{
		{"new table file", "", base, true, ""},
		{"add column", base, base + "ALTER TABLE orders ADD COLUMN note text;\n", true, ""},
		{"add column alone", "", "ALTER TABLE orders ADD COLUMN note text;\n", true, ""},
		{"add not null with default", base, base + "ALTER TABLE orders ADD COLUMN n int NOT NULL DEFAULT 0;\n", true, ""},
		{"default then not null", "", "ALTER TABLE orders ADD COLUMN n int DEFAULT 0 NOT NULL;\n", true, ""},
		{"concurrent index", base, base + "CREATE INDEX CONCURRENTLY orders_note ON orders (note);\n", true, ""},
		{"comment and blank lines only", base, "-- header\n\n" + base, true, ""},
		{"comments only", "-- a\n/* b */\n", "/* c */\n-- d\n", true, ""},
		{"whitespace and comments only", "ALTER TABLE orders ADD COLUMN note text;", "-- keep\nALTER   TABLE /* c */\norders ADD COLUMN note text ;", true, ""},
		{"unchanged drop is not a new statement", "DROP TABLE orders;", "DROP   TABLE\norders ;", true, ""},
		{"danger in a string", "", "COMMENT ON TABLE t IS 'drop table x';\n", true, ""},
		{"python ddl list grows", "DDL = [\n  \"CREATE TABLE a (x int)\",\n]\n", "DDL = [\n  \"CREATE TABLE a (x int)\",\n  \"CREATE TABLE b (y int)\",\n]\n", false, "unterminated statement"},
		{"drop", base, base + "DROP TABLE legacy;\n", false, "DROP"},
		{"drop one line", "", "DROP TABLE orders;\n", false, "DROP"},
		{"rename", base, base + "ALTER TABLE orders RENAME TO orders_v2;\n", false, "RENAME"},
		{"alter rename", "", "ALTER TABLE orders RENAME COLUMN note TO n;\n", false, "RENAME"},
		{"type change", base, base + "ALTER TABLE orders ALTER COLUMN id TYPE int;\n", false, "ALTER COLUMN"},
		{"alter column split", "", "ALTER TABLE orders ALTER\nCOLUMN amount TYPE integer;\n", false, "ALTER COLUMN"},
		{"alter column block comment", "", "ALTER TABLE orders ALTER /* x */ COLUMN amount TYPE integer;\n", false, "not an allowed"},
		{"truncate", base, base + "TRUNCATE orders;\n", false, "TRUNCATE"},
		{"grant", base, base + "GRANT SELECT ON orders TO reader;\n", false, "GRANT"},
		{"insert", "", "INSERT INTO orders (id) VALUES (1);\n", false, "INSERT"},
		{"create or replace", base, base + "CREATE OR REPLACE VIEW v AS SELECT 1;\n", false, "REPLACE"},
		{"create or replace function", "", "CREATE OR REPLACE FUNCTION f() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql;\n", false, "REPLACE"},
		{"blocking index", base, base + "CREATE INDEX orders_id ON orders (id);\n", false, "without CONCURRENTLY"},
		{"create index split", "", "CREATE\nINDEX orders_amount ON orders (amount);\n", false, "without CONCURRENTLY"},
		{"not null without default", base, base + "ALTER TABLE orders ADD COLUMN n int NOT NULL;\n", false, "NOT NULL column"},
		{"not null without default alone", "", "ALTER TABLE t ADD COLUMN c int NOT NULL;\n", false, "NOT NULL column"},
		{"row rewrite", base, base + "UPDATE orders SET id = id + 1;\n", false, "UPDATE"},
		{"delete split", "", "DELETE\nFROM orders;\n", false, "DELETE"},
		{"select pg_sleep", "", "SELECT pg_sleep(600);\n", false, "pg_sleep"},
		{"do execute drop", "", "DO $$ BEGIN EXECUTE 'DR' || 'OP TABLE orders'; END $$;\n", false, "not an allowed"},
		{"vacuum full", "", "VACUUM FULL orders;\n", false, "VACUUM"},
		{"create table as select", "", "CREATE TABLE t AS SELECT 1;\n", false, "not an allowed"},
		{"two actions", "", "ALTER TABLE orders ADD COLUMN a int, ADD COLUMN b int;\n", false, "not an allowed"},
		{"default random", "", "ALTER TABLE t ADD COLUMN c int DEFAULT random();\n", false, "not an allowed"},
		{"negative default", "", "ALTER TABLE t ADD COLUMN c int DEFAULT -1;\n", false, "not an allowed"},
		{"multiword type", "", "ALTER TABLE t ADD COLUMN c double precision;\n", false, "not an allowed"},
		{"set lock_timeout", "", "SET lock_timeout = 0;\n", false, "not an allowed"},
		{"removed statement", "CREATE TABLE a (id int);\nCREATE TABLE b (id int);\n", "CREATE TABLE a (id int);\n", false, "removes or rewrites"},
		{"reordered", reorderedBefore, reorderedAfter, false, "removes or rewrites"},
		{"rewritten line", base, "CREATE TABLE orders (id int PRIMARY KEY);\n", false, "removes or rewrites"},
		{"removed line", base + "CREATE TABLE b (y int);\n", base, false, "removes or rewrites"},
		{"unterminated dollar quote", "", "DO $$ begin", false, "unterminated dollar quote"},
		{"psql set", "", "\\set x 1\n", false, "psql meta-command"},
		{"hash is not a comment", "", "# comment\nALTER TABLE t ADD COLUMN c int;\n", false, "not SQL"},
		{"slash slash is not a comment", "", "// comment\n", false, "not SQL"},
		{"unterminated statement", "", "ALTER TABLE t ADD COLUMN c int\n", false, "unterminated statement"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ok, why := releasepolicy.ClassifyDDL(tc.before, tc.after)
			if ok != tc.ok || !strings.Contains(why, tc.why) {
				t.Fatalf("ClassifyDDL = %v %q, want %v containing %q", ok, why, tc.ok, tc.why)
			}
		})
	}
}

func TestAdditiveDDLShipsOnThePolicy(t *testing.T) {
	oldSHA, newSHA := rptest.SHA("old"), rptest.SHA("new")
	allowBefore := "CREATE TABLE orders (id bigint);\n"
	allowAfter := allowBefore + "ALTER TABLE orders ADD COLUMN note text;\n"

	t.Run("allowed sql", func(t *testing.T) {
		f := newFixture(t)
		f.facts.Files["repo-app"] = []string{"db/ddl_orders.sql", "docs/readme.md"}
		f.facts.Text["repo-app@"+oldSHA+":db/ddl_orders.sql"] = allowBefore
		f.facts.Text["repo-app@"+newSHA+":db/ddl_orders.sql"] = allowAfter
		d := f.decide(nil)
		wantAuto(t, d)
		if strings.Join(d.AdditiveDDL, ",") != "repo-app:db/ddl_orders.sql" || !strings.Contains(d.ClauseText(), "additive_ddl") ||
			strings.Contains(d.ClauseText(), ",no_ddl") {
			t.Fatalf("additive = %q clauses = %s", d.AdditiveDDL, d.ClauseText())
		}
	})

	t.Run("refused sql waits for the owner", func(t *testing.T) {
		f := newFixture(t)
		f.facts.Files["repo-app"] = []string{"db/seed.sql"}
		f.facts.Text["repo-app@"+oldSHA+":db/seed.sql"] = allowBefore
		f.facts.Text["repo-app@"+newSHA+":db/seed.sql"] = allowBefore + "DROP TABLE orders;\n"
		d := f.decide(nil)
		wantWait(t, d, "ask the Owner")
		if d.Auto || !strings.Contains(strings.Join(d.Reasons, "\n"), "not an allowed statement") {
			t.Fatalf("reasons %q", d.Reasons)
		}
	})

	t.Run("python waits for the owner", func(t *testing.T) {
		f := newFixture(t)
		f.facts.Files["repo-app"] = []string{"db/ddl_orders.py"}
		f.facts.Text["repo-app@"+oldSHA+":db/ddl_orders.py"] = "DDL = [\n  \"CREATE TABLE a (x int)\",\n]\n"
		f.facts.Text["repo-app@"+newSHA+":db/ddl_orders.py"] = "DDL = [\n  \"CREATE TABLE a (x int)\",\n  \"CREATE TABLE b (y int)\",\n]\n"
		d := f.decide(nil)
		wantWait(t, d, "ask the Owner")
		if !strings.Contains(strings.Join(d.Reasons, "\n"), "db/ddl_orders.py is not a .sql file") {
			t.Fatalf("reasons %q", d.Reasons)
		}
	})

	t.Run("migrations sql waits for the owner", func(t *testing.T) {
		f := newFixture(t)
		f.facts.Files["repo-app"] = []string{"migrations/0001_add.sql"}
		f.facts.Text["repo-app@"+oldSHA+":migrations/0001_add.sql"] = allowBefore
		f.facts.Text["repo-app@"+newSHA+":migrations/0001_add.sql"] = allowAfter
		wantWait(t, f.decide(nil), "migrations/0001_add.sql is under migrations/")
	})

	t.Run("db_init sql waits for the owner", func(t *testing.T) {
		f := newFixture(t)
		f.facts.Files["repo-app"] = []string{"scripts/db_init.sql"}
		f.facts.Text["repo-app@"+oldSHA+":scripts/db_init.sql"] = allowBefore
		f.facts.Text["repo-app@"+newSHA+":scripts/db_init.sql"] = allowAfter
		wantWait(t, f.decide(nil), "scripts/db_init.sql matches db_init*")
	})

	// Without additive_ddl in the signed policy, any DDL hit waits as no_ddl.
	t.Run("without additive_ddl", func(t *testing.T) {
		f := newFixture(t)
		f.facts.Files["repo-app"] = []string{"db/ddl_orders.sql"}
		f.facts.Text["repo-app@"+oldSHA+":db/ddl_orders.sql"] = allowBefore
		f.facts.Text["repo-app@"+newSHA+":db/ddl_orders.sql"] = allowAfter
		strict := strings.Replace(rptest.PolicyText("rp-strict", now.Add(-time.Hour), 90, pipeline), `"additive_ddl": true`, `"additive_ddl": false`, 1)
		f.signPolicy(t, f.key, strict)
		wantWait(t, f.decide(nil), "hits no_ddl: db/ddl_orders.sql")
	})
}

func TestPinnedReleaseMustShipWhatTheSourceShipped(t *testing.T) {
	f := newFixture(t)
	oldSHA, newSHA := rptest.SHA("old"), rptest.SHA("new")
	oldLib, newLib := rptest.SHA("old-lib"), rptest.SHA("new-lib")
	f.signPolicy(t, f.key, rptest.PolicyText("rp-pinned", now.Add(-time.Hour), 90, "deliver-pinned"))
	f.facts.Records["deliver-pinned"] = map[string]string{"repo-app": oldSHA, "repo-lib": oldLib}
	f.facts.Records["deliver-app-stg"] = map[string]string{"repo-app": newSHA, "repo-lib": newLib}
	f.facts.Green["repo-lib@"+newLib] = true
	f.facts.Files["repo-lib"] = []string{"lib/util.go"}
	pinned := func(app, lib string) map[string]any {
		return map[string]any{"name": "deliver-pinned", "revision": lib, "who": "agent@mac",
			"params": map[string]any{"appRevision": app}}
	}
	// main has moved past the STG commits; the pinned commits are what count.
	f.facts.Heads["repo-app"] = rptest.SHA("later")
	d := f.decide(pinned(newSHA, newLib))
	wantAuto(t, d)
	if !strings.Contains(d.ClauseText(), "pinned_to:deliver-app-stg") || d.SHAText() != "repo-app="+newSHA+",repo-lib="+newLib {
		t.Fatalf("clauses = %s sha = %s", d.ClauseText(), d.SHAText())
	}
	wantWait(t, f.decide(pinned(rptest.SHA("other"), newLib)), "is not what the newest deliver-app-stg record shipped")
	wantWait(t, f.decide(pinned(newSHA, "main")), "repo-lib: pinned revision main is not a full commit id")
	wantWait(t, f.decide(map[string]any{"name": "deliver-pinned", "revision": newLib, "who": "agent@mac"}), "repo-app: the request pins no commit for it")
	wantWait(t, f.decide(map[string]any{"name": "deliver-pinned", "who": "agent@mac"}), "names none")
	f.facts.Green = nil
	wantWait(t, f.decide(pinned(newSHA, newLib)), "no Succeeded ci run")
}

func TestEvaluateCoversTierBWithoutTheTierRule(t *testing.T) {
	f := newFixture(t)
	params := map[string]any{"name": pipeline, "revision": "main", "who": "agent@mac"}
	ctx := context.Background()
	wantWait(t, f.eng.Decide(ctx, "start_pipeline_run", actions.TierB, params), "only tier C")
	wantAuto(t, f.eng.Evaluate(ctx, "start_pipeline_run", params))
	f.facts.Window = "no release window is open"
	wantWait(t, f.eng.Evaluate(ctx, "start_pipeline_run", params), "release window")
}

func TestFreezeSetIgnoresAMissingConfigMap(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if frozen, _ := f.eng.FreezeSet(ctx); frozen {
		t.Fatal("frozen=false reads as set")
	}
	f.cms.Set(releasepolicy.FreezeConfigMap, nil)
	if frozen, _ := f.eng.FreezeSet(ctx); frozen {
		t.Fatal("a missing freeze ConfigMap stops tier B releases")
	}
	f.cms.Set(releasepolicy.FreezeConfigMap, map[string]string{"frozen": "true", "frozen_at": "2026-10-08T11:00:00Z", "who": "a", "reason": "b"})
	if frozen, reason := f.eng.FreezeSet(ctx); !frozen || !strings.Contains(reason, "frozen by a") {
		t.Fatalf("set freeze = %v %q", frozen, reason)
	}
}
