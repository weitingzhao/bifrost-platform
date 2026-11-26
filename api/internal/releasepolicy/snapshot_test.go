package releasepolicy_test

// Decision snapshots: how the engine answers a small corpus of requests,
// kept as golden files under testdata/decisions.
//
// When you change how the engine judges, some of these answers change. Run
//
//	go test ./internal/releasepolicy -run TestDecisionSnapshots -update
//
// and commit the rewritten files with the change. Their diff is your
// statement of which answers changed; read it before you commit. An answer
// that went from refused to covered without your meaning it to is a bug.
// The requests that must stay refused whatever changes are in mustrefuse/,
// which only the Owner edits.

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

var updateSnapshots = flag.Bool("update", false, "rewrite testdata/decisions from what the engine answers now")

// snapshotCase is one request of the corpus: the world it is asked in (the
// fixture's covered release and covered apply, edited) and the request.
// params nil is the fixture's release of main.
type snapshotCase struct {
	name   string
	world  func(f *applyFixture)
	action string
	tier   releasepolicy.Tier
	params map[string]any
}

func snapshotCases() []snapshotCase {
	release := func(name string, world func(f *applyFixture)) snapshotCase {
		return snapshotCase{name: name, world: world, action: "start_pipeline_run", tier: releasepolicy.Tier("C")}
	}
	apply := func(name string, world func(f *applyFixture)) snapshotCase {
		return snapshotCase{name: name, world: world, action: "apply_manifest", tier: releasepolicy.Tier("C"), params: map[string]any{"plan_id": "plan-1"}}
	}
	files := func(list ...string) func(f *applyFixture) {
		return func(f *applyFixture) { f.facts.Files["repo-app"] = list }
	}
	oldLib, newLib := rptest.SHA("old-lib"), rptest.SHA("new-lib")
	pinned := func(name string, world func(f *applyFixture)) snapshotCase {
		return snapshotCase{name: name, action: "start_pipeline_run", tier: releasepolicy.Tier("C"),
			params: map[string]any{"name": "deliver-pinned", "revision": newLib, "who": "agent@mac", "params": map[string]any{"appRevision": rptest.SHA("new")}},
			world: func(f *applyFixture) {
				f.facts.Records["deliver-pinned"] = map[string]string{"repo-app": rptest.SHA("old"), "repo-lib": oldLib}
				f.facts.Records["deliver-app-stg"] = map[string]string{"repo-app": rptest.SHA("later"), "repo-lib": newLib}
				f.facts.Green["repo-lib@"+newLib] = true
				f.facts.Files["repo-lib"] = []string{"lib/util.go"}
				world(f)
			}}
	}
	migration := func(name string, world func(f *applyFixture)) snapshotCase {
		return snapshotCase{name: name, world: world, action: "run_agentrt_migration", tier: releasepolicy.Tier("C"), params: map[string]any{"commit": rptest.SHA("new")}}
	}
	return []snapshotCase{
		release("release-covered", nil),
		release("release-reversible-migration", func(f *applyFixture) { migrationWorld(f, inClassSQL) }),
		release("refused-migration-drop-table", func(f *applyFixture) { migrationWorld(f, "DROP TABLE orders;\n") }),
		migration("migration-covered", func(f *applyFixture) { migrationWorld(f, inClassSQL) }),
		migration("migration-not-covered", func(f *applyFixture) {
			migrationWorld(f, "CREATE TABLE notes (id int);\nDROP TABLE orders;\n")
		}),
		release("release-self-checked-trust-anchor", files("docs/readme.md", anchorFile)),
		release("release-additive-ddl", func(f *applyFixture) {
			f.facts.Files["repo-app"] = []string{"db/ddl_orders.py"}
			f.facts.Text["repo-app@"+rptest.SHA("new")+":db/ddl_orders.py"] = "ALTER TABLE orders ADD COLUMN note text;\n"
		}),
		release("release-large-diff-read-whole", func(f *applyFixture) {
			f.facts.Files["repo-app"] = manyFiles(120)
			f.facts.TreeFiles = map[string][]string{"repo-app": manyFiles(120)}
		}),
		pinned("pinned-older-source-record", func(f *applyFixture) {
			f.facts.Older = map[string][]releasepolicy.Shipment{"deliver-app-stg": {
				{Repos: map[string]string{"repo-app": rptest.SHA("new"), "repo-lib": newLib}, Succeeded: true},
			}}
		}),
		pinned("pinned-never-delivered", func(*applyFixture) {}),
		apply("apply-covered", nil),
		apply("apply-first-creation", func(f *applyFixture) { delete(f.live.Objects, "cicd/Pipeline/ci") }),
		apply("apply-secret", func(f *applyFixture) {
			f.editPlan(func(p *releasepolicy.ApplyPlan) { p.Objects[0].Kind = "Secret" })
		}),
		release("refused-frozen", func(f *applyFixture) {
			f.cms.Set(releasepolicy.FreezeConfigMap, map[string]string{"frozen": "true", "frozen_at": "2026-10-08T11:30:00Z", "who": "owner", "reason": "incident"})
		}),
		release("refused-expired", func(f *applyFixture) { f.clock = now.Add(91 * 24 * time.Hour) }),
		{name: "refused-pipeline-not-allowed", action: "start_pipeline_run", tier: releasepolicy.Tier("C"),
			params: map[string]any{"name": "deliver-elsewhere", "revision": "main", "who": "agent@mac"}},
		{name: "refused-tier-d", action: "start_pipeline_run", tier: releasepolicy.Tier("D"),
			params: map[string]any{"name": pipeline, "revision": "main", "who": "agent@mac"}},
		release("refused-window", func(f *applyFixture) { f.facts.Window = "held by another session" }),
		release("refused-ci", func(f *applyFixture) { f.facts.Green = nil }),
		release("refused-d10-path", files("engine/orders/place.go")),
		release("refused-ddl-drop", func(f *applyFixture) {
			f.facts.Files["repo-app"] = []string{"db/seed.sql"}
			f.facts.Text["repo-app@"+rptest.SHA("new")+":db/seed.sql"] = "DROP TABLE orders;\n"
		}),
		release("refused-owner-only-path", files(anchorFile, ownerOnlyFile)),
		release("refused-trust-anchor-ci-red", func(f *applyFixture) {
			f.facts.Files["repo-app"] = []string{anchorFile}
			f.facts.Green = nil
		}),
		release("refused-large-diff-unreadable", func(f *applyFixture) { f.facts.Files["repo-app"] = manyFiles(120) }),
	}
}

// LAW-9: no second reader; the author proves a judge change with
// machine facts (must-refuse set, decision snapshots). Every answer of the
// corpus is compared whole with its golden file: covered or not, the clauses
// in the order checked, every reason.
func TestDecisionSnapshots(t *testing.T) {
	dir := filepath.Join("testdata", "decisions")
	want := map[string]bool{}
	for _, tc := range snapshotCases() {
		want[tc.name+".json"] = true
		t.Run(tc.name, func(t *testing.T) {
			f := newApplyFixture(t, func(doc map[string]any) {
				selfChecked(doc)
				reversible(doc)
			})
			if tc.world != nil {
				tc.world(f)
			}
			params := tc.params
			if params == nil {
				params = map[string]any{"name": pipeline, "revision": "main", "who": "agent@mac"}
			}
			d := f.eng.Decide(context.Background(), tc.action, tc.tier, params)
			got, err := json.MarshalIndent(d, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')
			path := filepath.Join(dir, tc.name+".json")
			if *updateSnapshots {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			golden, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("no snapshot (run with -update, then read the diff): %v", err)
			}
			if string(golden) != string(got) {
				t.Fatalf("the engine's answer changed. If you meant it, run with -update and commit the diff.\n--- %s\n%s--- now\n%s", path, golden, got)
			}
		})
	}
	// A snapshot no case writes is a stale answer nobody compares.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var stale []string
	for _, e := range entries {
		if !want[e.Name()] {
			stale = append(stale, e.Name())
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Fatalf("snapshots without a case (delete them): %v", stale)
	}
}
