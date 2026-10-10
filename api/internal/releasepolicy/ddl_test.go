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
	for _, tc := range []struct {
		name, before, after string
		ok                  bool
		why                 string
	}{
		{"new table file", "", base, true, ""},
		{"add column", base, base + "ALTER TABLE orders ADD COLUMN note text;\n", true, ""},
		{"add not null with default", base, base + "ALTER TABLE orders ADD COLUMN n int NOT NULL DEFAULT 0;\n", true, ""},
		{"concurrent index", base, base + "CREATE INDEX CONCURRENTLY orders_note ON orders (note);\n", true, ""},
		{"comment and blank lines only", base, "-- header\n\n" + base, true, ""},
		{"python ddl list grows", "DDL = [\n  \"CREATE TABLE a (x int)\",\n]\n", "DDL = [\n  \"CREATE TABLE a (x int)\",\n  \"CREATE TABLE b (y int)\",\n]\n", true, ""},
		{"drop", base, base + "DROP TABLE legacy;\n", false, "adds DROP"},
		{"rename", base, base + "ALTER TABLE orders RENAME TO orders_v2;\n", false, "adds RENAME"},
		{"type change", base, base + "ALTER TABLE orders ALTER COLUMN id TYPE int;\n", false, "ALTER COLUMN"},
		{"truncate", base, base + "TRUNCATE orders;\n", false, "TRUNCATE"},
		{"grant", base, base + "GRANT SELECT ON orders TO reader;\n", false, "GRANT"},
		{"create or replace", base, base + "CREATE OR REPLACE VIEW v AS SELECT 1;\n", false, "REPLACE"},
		{"blocking index", base, base + "CREATE INDEX orders_id ON orders (id);\n", false, "without CONCURRENTLY"},
		{"not null without default", base, base + "ALTER TABLE orders ADD COLUMN n int NOT NULL;\n", false, "NOT NULL column"},
		{"row rewrite", base, base + "UPDATE orders SET id = id + 1;\n", false, "UPDATE"},
		{"rewritten line", base, "CREATE TABLE orders (id int PRIMARY KEY);\n", false, "removes or rewrites"},
		{"removed line", base + "CREATE TABLE b (y int);\n", base, false, "removes or rewrites"},
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
	f := newFixture(t)
	oldSHA, newSHA := rptest.SHA("old"), rptest.SHA("new")
	f.facts.Files["repo-app"] = []string{"db/ddl_orders.py", "docs/readme.md"}
	f.facts.Text = map[string]string{
		"repo-app@" + oldSHA + ":db/ddl_orders.py": "CREATE TABLE orders (id bigint);\n",
		"repo-app@" + newSHA + ":db/ddl_orders.py": "CREATE TABLE orders (id bigint);\nALTER TABLE orders ADD COLUMN note text;\n",
	}
	d := f.decide(nil)
	wantAuto(t, d)
	if strings.Join(d.AdditiveDDL, ",") != "repo-app:db/ddl_orders.py" || !strings.Contains(d.ClauseText(), "additive_ddl") ||
		strings.Contains(d.ClauseText(), ",no_ddl") {
		t.Fatalf("additive = %q clauses = %s", d.AdditiveDDL, d.ClauseText())
	}

	// Without additive_ddl in the signed policy, any DDL hit waits.
	strict := strings.Replace(rptest.PolicyText("rp-strict", now.Add(-time.Hour), 90, pipeline), `"additive_ddl": true`, `"additive_ddl": false`, 1)
	f.signPolicy(t, f.key, strict)
	wantWait(t, f.decide(nil), "hits no_ddl: db/ddl_orders.py")
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
