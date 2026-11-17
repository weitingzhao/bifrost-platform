//bifrost:owner verification

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

// The contract between the units routes and the Console's CI views: the
// bodies in testdata/contract/units are what these routes answer, and the
// Console's tests read the same files instead of this package's source.
// Origin: the Owner, 2026-11-14: "CI has its own front end and back end now;
// what it asks of the products that run on it, it asks of itself", and the
// declaration standard's rule that a contract is one copy, kept by the side
// that provides it, compared on every run. Each body is computed by the code
// the route runs and compared with its file; UNITS_CONTRACT_WRITE=1 writes
// the files instead, for a change of the contract made on purpose (the
// Console's tests then run against the new files).
func unitsContract(t *testing.T, name string, body any) {
	t.Helper()
	got, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", "contract", "units", name)
	if os.Getenv("UNITS_CONTRACT_WRITE") == "1" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (UNITS_CONTRACT_WRITE=1 writes it)", name, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s is not what the route answers now. The Console's CI views are tested against that file: change both on purpose (UNITS_CONTRACT_WRITE=1), not one.\n--- now\n%s", name, got)
	}
}

func TestUnitsContractConfig(t *testing.T) {
	t.Parallel()
	const manifest = `version: 1
pins:
  lib: {commit: "1111111111111111111111111111111111111111", advance: manual}
modules:
  - name: svc
    kind: go
    root: svc
    image: example.invalid/go:1
    template: run-db
    tasks: {test: {per: package}, vet: {per: module}}
    shards: {svc/a: 3}
    inputs:
      svc/a: ["."]
      svc/b: ["web/index.ts"]
  - name: web
    root: web
    needs: ["svc/a/a.go", "lib:."]
    tasks: {check: {}}
`
	files := map[string]string{"bifrost.units.yaml": manifest, "svc/go.mod": "module example.com/m\n\ngo 1.25\n",
		"svc/a/a.go": "package a\n", "svc/a/a_test.go": "package a\n", "svc/b/b.go": "package b\n", "svc/b/b_test.go": "package b\n", "web/index.ts": "1"}
	c, lib := strings.Repeat("c", 40), strings.Repeat("1", 40)
	trees := scopeTrees(t, map[string]map[string]string{c: files, lib: {"x.ts": "x"}})
	declared, err := unitConfig(context.Background(), func(ctx context.Context, _, commit string) (*unitplan.Tree, error) {
		return trees(ctx, "r", commit) // the pinned repository's tree is read by its commit alone here
	}, "r", c)
	if err != nil || !declared.Declared || len(declared.Modules) != 2 {
		t.Fatalf("declared: %+v %v", declared, err)
	}
	unitsContract(t, "config-declared.json", declared)
	files["config/domain-ownership.json"] = `{"version":1,"source":"contract fixture","domains":[{"id":"plan","name":"Planning","source":"planning charter"},{"id":"foundation","name":"Foundation","source":"default ruling"}],"default":{"domains":["foundation"],"status":"default-maintainer","basis":"default ruling"},"packages":{"svc/a":{"domains":["plan"],"status":"assigned","basis":"planning charter"}}}`
	var domainDoc map[string]any
	if err := json.Unmarshal([]byte(files["config/domain-ownership.json"]), &domainDoc); err != nil {
		t.Fatal(err)
	}
	domainDoc["frontend"] = map[string]any{"root": "web", "inventory": "frontend-inventory.json", "groups": map[string]any{"web": map[string]any{"domains": []string{"plan"}, "status": "assigned", "basis": "frontend plan view source audit"}}}
	domainRaw, _ := json.Marshal(domainDoc)
	files["config/domain-ownership.json"] = string(domainRaw)
	files["frontend-inventory.json"] = `{"version":1,"root":"web","groups":[{"path":"web","files":["web/index.ts"],"imports":[],"test_imports":[],"external_imports":["react"],"source_inputs":[],"test_inputs":[],"unresolved_imports":[]}]}`
	mapped, err := unitConfig(context.Background(), func(ctx context.Context, _, commit string) (*unitplan.Tree, error) { return trees(ctx, "r", commit) }, "r", c)
	if err != nil || mapped.Domains == nil || mapped.DomainNote != "" {
		t.Fatalf("mapping: %+v %v", mapped, err)
	}
	unitsContract(t, "config-domains.json", mapped)
	delete(files, "config/domain-ownership.json")

	delete(files, "bifrost.units.yaml")
	undeclared, err := unitConfig(context.Background(), scopeTrees(t, map[string]map[string]string{c: files}), "r", c)
	if err != nil || undeclared.Declared {
		t.Fatalf("undeclared: %+v %v", undeclared, err)
	}
	unitsContract(t, "config-undeclared.json", undeclared)
}

func TestUnitsContractRuns(t *testing.T) {
	t.Parallel()
	// The kinds of run seen on 2026-11-14: a landing's run that left a module
	// out, the run of main's push that reused it, a daily full run, a run
	// still going, and a run of a pipeline that does not ask.
	runs := []landingci.RunView{
		{Name: "ci-5", Pipeline: "ci-pipeline", Revision: strings.Repeat("5", 40), Status: "Running", Trigger: "gitea-webhook", CreatedAt: "2026-11-14T16:40:00Z",
			Asked: true, RunModules: "*", ScopeWhy: "the platform says every module runs", Tasks: []landingci.TaskView{{Task: "changes", Status: "Succeeded", Seconds: 9, Waited: 6}, {Task: "test-web", Status: "Running", Waited: 5}}},
		{Name: "ci-full-444444444444-20261114", Pipeline: "ci-pipeline", Revision: strings.Repeat("4", 40), Status: "Failed", Trigger: landingci.FullCITrigger, Full: true,
			CreatedAt: "2026-11-14T09:00:00Z", Seconds: 215, Asked: true, RunModules: "*", ScopeWhy: "not asked: the changes against main were not read",
			Tasks: []landingci.TaskView{{Task: "changes", Status: "Succeeded", Seconds: 8, Waited: 6}, {Task: "test-svc", Status: "Succeeded", Seconds: 130, Waited: 7}, {Task: "test-web", Status: "Failed", Seconds: 199, Waited: 6}}},
		{Name: "ci-3", Pipeline: "ci-pipeline", Revision: strings.Repeat("3", 40), Status: "Succeeded", Trigger: "gitea-webhook", CreatedAt: "2026-11-14T08:05:00Z", Seconds: 25,
			TestedBy: "ci-2", Asked: true, RunModules: "*", ScopeWhy: "not asked: the changes against main were not read", Tasks: []landingci.TaskView{{Task: "changes", Status: "Succeeded", Seconds: 8, Waited: 6}}},
		{Name: "ci-2", Pipeline: "ci-pipeline", Revision: strings.Repeat("3", 40), Status: "Succeeded", Trigger: "gitea-webhook", CreatedAt: "2026-11-14T08:00:00Z", Seconds: 136,
			Asked: true, RunModules: " svc repo ", ScopeWhy: "not run, their keys are the base's: web",
			Tasks: []landingci.TaskView{{Task: "changes", Status: "Succeeded", Seconds: 11, Waited: 6}, {Task: "test-svc", Status: "Succeeded", Seconds: 123, Waited: 7}, {Task: "test-web", Status: "Succeeded", Seconds: 12, Waited: 5}}},
		{Name: "ci-1", Pipeline: "another-pipeline", Revision: strings.Repeat("2", 40), Status: "Succeeded", Trigger: "platform-api", CreatedAt: "2026-11-13T20:00:00Z", Seconds: 300, Tasks: []landingci.TaskView{}},
	}
	var cache unitRunsCache
	out, err := cache.unitRuns(context.Background(), func(context.Context, string, int) ([]landingci.RunView, error) { return runs, nil },
		time.Date(2026, 11, 14, 16, 45, 0, 0, time.UTC), "r", unitRunsDefault)
	if err != nil {
		t.Fatal(err)
	}
	unitsContract(t, "runs.json", out)
	empty, err := cache.unitRuns(context.Background(), func(context.Context, string, int) ([]landingci.RunView, error) { return []landingci.RunView{}, nil },
		time.Date(2026, 11, 14, 16, 45, 0, 0, time.UTC), "nobody", unitRunsDefault)
	if err != nil {
		t.Fatal(err)
	}
	unitsContract(t, "runs-none.json", empty)
}

func TestUnitsContractFullCI(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	if _, err := store.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pool := db.Pool(t, db.Runtime)
	unitsContract(t, "full-ci-none.json", unitFullCIAnswer([]evidence.FullCINote{}))
	notes := []struct {
		text string
		body evidence.FullCI
	}{
		{"Full CI started: ci-full-444444444444-20261113.", evidence.FullCI{Started: []string{"ci-full-444444444444-20261113"}}},
		{"Full CI started: ci-full-111111111111-20261114.", evidence.FullCI{Started: []string{"ci-full-111111111111-20261114"}}},
		{"Full CI run ci-full-111111111111-20261114 of 1111111111111111111111111111111111111111 ended red; failed tasks: test-web. 1 commit(s) landed since the last green full run ci-full-444444444444-20261113 with a module left out of their own CI run: 33333333 ( svc repo ); the CI run of 1 more could not be read.",
			evidence.FullCI{Run: "ci-full-111111111111-20261114", Commit: strings.Repeat("1", 40), FailedTasks: []string{"test-web"},
				Suspects: []evidence.FullCISuspect{{Commit: strings.Repeat("3", 40), Subject: "feat: a change to svc alone", Run: "ci-2", Modules: " svc repo "}}, Unread: 1}},
		{"Full CI run ci-full-222222222222-20261115 of 2222222222222222222222222222222222222222 ended red; no failed task could be read. Which landed commits had a module left out of their own CI run was not reconciled: no full run in the last 3 days was green.",
			evidence.FullCI{Run: "ci-full-222222222222-20261115", Commit: strings.Repeat("2", 40), FailedKey: "TestA TestB", Unreconciled: "no full run in the last 3 days was green"}},
	}
	for _, n := range notes {
		if err := evidence.NoteFullCI(ctx, pool, n.text, n.body); err != nil {
			t.Fatal(err)
		}
	}
	read, err := evidence.FullCINotes(ctx, pool, unitFullCINotes)
	if err != nil || len(read) != len(notes) {
		t.Fatalf("notes: %d %v", len(read), err)
	}
	// The time a note was written is the database's: one fixed instant a note, newest first, for the file.
	for i := range read {
		read[i].At = time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC).Add(-time.Duration(i) * time.Hour)
	}
	unitsContract(t, "full-ci.json", unitFullCIAnswer(read))
}

// The scope route's body: the modules that run, and under each the units
// that run and those whose keys are the base's (run_units, same_units).
// Origin: main coordinator 2026-11-14: backend skip by package goes shadow
// first; a failing package the scope would skip is the miss this looks for.
// The module-level fields are what CI acts on and are as they were.
func TestUnitsContractScope(t *testing.T) {
	t.Parallel()
	const manifest = `version: 1
modules:
  - name: svc
    kind: go
    root: svc
    tasks: {test: {per: package}}
    inputs:
      svc/c: ["docs/c.md"]
  - name: web
    kind: node
    root: web
    tasks: {test: {}}
`
	base := map[string]string{"bifrost.units.yaml": manifest, "svc/go.mod": "module example.com/m\n\ngo 1.25\n",
		"svc/a/a.go": "package a\n\nimport _ \"example.com/m/b\"\n", "svc/a/a_test.go": "package a\n",
		"svc/b/b.go": "package b\n", "svc/b/b_test.go": "package b\n",
		"svc/c/c.go": "package c\n", "svc/c/c_test.go": "package c\n",
		"svc/d/d.go": "package d\n", "svc/d/d_test.go": "package d\n", "docs/c.md": "c", "web/index.ts": "1"}
	head := map[string]string{}
	for k, v := range base {
		head[k] = v
	}
	head["svc/b/b.go"], head["docs/c.md"] = "package b\n\nconst B = 1\n", "c2"
	b, c := strings.Repeat("b", 40), strings.Repeat("c", 40)
	trees := scopeTrees(t, map[string]map[string]string{b: base, c: head})
	passed := func(context.Context, string, string) (string, string, error) { return "Succeeded", "ci-1", nil }
	got, err := unitScope(context.Background(), trees, passed, "r", c, b)
	if err != nil {
		t.Fatal(err)
	}
	// A package's own change runs it and its importer; a file a package lists runs that package; the rest are the base's.
	if m := got.Modules[0]; strings.Join(m.RunUnits, " ") != "svc/a svc/b svc/c" || strings.Join(m.SameUnits, " ") != "svc/d" {
		t.Errorf("svc: run %v same %v", m.RunUnits, m.SameUnits)
	}
	unitsContract(t, "scope.json", got)
	// An unreadable base: every unit runs, with the reason.
	whole, err := unitScope(context.Background(), scopeTrees(t, map[string]map[string]string{b: {"x": "1"}, c: head}), passed, "r", c, b)
	if err != nil || whole.Note == "" || strings.Join(whole.Modules[0].RunUnits, " ") != "svc/a svc/b svc/c svc/d" || len(whole.Modules[0].SameUnits) != 0 {
		t.Fatalf("whole: %+v %v", whole, err)
	}
	unitsContract(t, "scope-whole.json", whole)
}
