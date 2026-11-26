package releasepolicy_test

import (
	"context"
	"strings"
	"testing"
	"time"

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
	// The action catalog hands the nested params over as map[string]string.
	normalised := pinned(newSHA, newLib)
	normalised["params"] = map[string]string{"appRevision": newSHA}
	if d := f.decide(normalised); !d.Auto || d.SHAText() != "repo-app="+newSHA+",repo-lib="+newLib {
		t.Fatalf("normalised params: auto = %v sha = %s reasons = %v", d.Auto, d.SHAText(), d.Reasons)
	}
	wantWait(t, f.decide(pinned(rptest.SHA("other"), newLib)), "was never delivered successfully by deliver-app-stg")
	wantWait(t, f.decide(pinned(newSHA, "main")), "repo-lib: pinned revision main is not a full commit id")
	wantWait(t, f.decide(map[string]any{"name": "deliver-pinned", "revision": newLib, "who": "agent@mac"}), "repo-app: the request pins no commit for it")
	wantWait(t, f.decide(map[string]any{"name": "deliver-pinned", "who": "agent@mac"}), "names none")
	f.facts.Green = nil
	wantWait(t, f.decide(pinned(newSHA, newLib)), "no Succeeded ci run")
}

// ISSUE-3 (2026-11-15): a card must not go stale because STG moved on (#568
// waited 5h31m and every lane stopped). A pinned release is vouched for by
// any successful record of the source pipeline that shipped exactly its
// commits, not by the newest record alone.
func TestPinnedReleaseIsVouchedForByAnySuccessfulSourceRecord(t *testing.T) {
	oldSHA, newSHA := rptest.SHA("old"), rptest.SHA("new")
	oldLib, newLib := rptest.SHA("old-lib"), rptest.SHA("new-lib")
	shipped := map[string]string{"repo-app": newSHA, "repo-lib": newLib}
	later := map[string]string{"repo-app": rptest.SHA("later"), "repo-lib": newLib}
	world := func(t *testing.T, newest map[string]string, older ...releasepolicy.Shipment) *fixture {
		f := newFixture(t)
		f.signPolicy(t, f.key, rptest.PolicyText("rp-pinned", now.Add(-time.Hour), 90, "deliver-pinned"))
		f.facts.Records["deliver-pinned"] = map[string]string{"repo-app": oldSHA, "repo-lib": oldLib}
		if newest != nil {
			f.facts.Records["deliver-app-stg"] = newest
		}
		f.facts.Older = map[string][]releasepolicy.Shipment{"deliver-app-stg": older}
		f.facts.Green["repo-lib@"+newLib] = true
		f.facts.Files["repo-lib"] = []string{"lib/util.go"}
		return f
	}
	request := map[string]any{"name": "deliver-pinned", "revision": newLib, "who": "agent@mac", "params": map[string]any{"appRevision": newSHA}}
	never := "repo-app=" + newSHA[:12] + ", repo-lib=" + newLib[:12] + " was never delivered successfully by deliver-app-stg"

	t.Run("the newest record shipped them: covered, as before", func(t *testing.T) {
		wantAuto(t, world(t, shipped).decide(request))
	})
	t.Run("an older successful record shipped them and the source has moved on: covered", func(t *testing.T) {
		d := world(t, later, releasepolicy.Shipment{Repos: shipped, Succeeded: true}).decide(request)
		wantAuto(t, d)
		if !strings.Contains(d.ClauseText(), "pinned_to:deliver-app-stg") {
			t.Fatalf("clauses = %s", d.ClauseText())
		}
	})
	t.Run("no record shipped them: refused", func(t *testing.T) {
		wantWait(t, world(t, later, releasepolicy.Shipment{Repos: map[string]string{"repo-app": oldSHA, "repo-lib": oldLib}, Succeeded: true}).decide(request), never)
	})
	t.Run("the source has no record at all: refused", func(t *testing.T) {
		wantWait(t, world(t, nil).decide(request), never)
	})
	t.Run("the record that shipped them did not succeed: refused", func(t *testing.T) {
		wantWait(t, world(t, later, releasepolicy.Shipment{Repos: shipped}).decide(request),
			"the deliver-app-stg record that shipped repo-app="+newSHA[:12]+", repo-lib="+newLib[:12]+" did not succeed")
	})
	// The commits must have shipped together: one record per repo vouches for neither.
	t.Run("each commit shipped, but never in one record: refused", func(t *testing.T) {
		wantWait(t, world(t, later, releasepolicy.Shipment{Repos: map[string]string{"repo-app": newSHA, "repo-lib": oldLib}, Succeeded: true}).decide(request), never)
	})
	// The bound: the newest 50 records are read. A match at the 50th counts,
	// one at the 51st does not.
	fill := func(n int) []releasepolicy.Shipment {
		out := make([]releasepolicy.Shipment, n)
		for i := range out {
			out[i] = releasepolicy.Shipment{Repos: later, Succeeded: true}
		}
		return append(out, releasepolicy.Shipment{Repos: shipped, Succeeded: true})
	}
	t.Run("a match at the last record read: covered", func(t *testing.T) {
		wantAuto(t, world(t, later, fill(48)...).decide(request))
	})
	t.Run("a match beyond the records read: refused", func(t *testing.T) {
		wantWait(t, world(t, later, fill(49)...).decide(request), "none of its newest 50 records shipped them")
	})
}

func TestEvaluateCoversTierBWithoutTheTierRule(t *testing.T) {
	f := newFixture(t)
	params := map[string]any{"name": pipeline, "revision": "main", "who": "agent@mac"}
	ctx := context.Background()
	wantWait(t, f.eng.Decide(ctx, "start_pipeline_run", releasepolicy.Tier("B"), params), "only tier C")
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
