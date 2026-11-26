// Moved from server (units_contract_test.go) on 2026-11-23, with UnitConfig;
// the contract files stay under api/internal/server/testdata/contract/units.

package assembly

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

// The contract between the units routes and the Console's CI views: the
// bodies in testdata/contract/units are what these routes answer, and the
// Console's tests read the same files instead of this package's source.
// Origin: ISSUE-34 (2026-11-14): CI has its own front end and back end now,
// and what it asks of the products that run on it it asks of itself; and the
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
	path := filepath.Join("..", "..", "server", "testdata", "contract", "units", name)
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
	declared, err := UnitConfig(context.Background(), func(ctx context.Context, _, commit string) (*unitplan.Tree, error) {
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
	mapped, err := UnitConfig(context.Background(), func(ctx context.Context, _, commit string) (*unitplan.Tree, error) { return trees(ctx, "r", commit) }, "r", c)
	if err != nil || mapped.Domains == nil || mapped.DomainNote != "" {
		t.Fatalf("mapping: %+v %v", mapped, err)
	}
	unitsContract(t, "config-domains.json", mapped)
	delete(files, "config/domain-ownership.json")

	delete(files, "bifrost.units.yaml")
	undeclared, err := UnitConfig(context.Background(), scopeTrees(t, map[string]map[string]string{c: files}), "r", c)
	if err != nil || undeclared.Declared {
		t.Fatalf("undeclared: %+v %v", undeclared, err)
	}
	unitsContract(t, "config-undeclared.json", undeclared)
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
	got, err := unitplan.ScopeOf(context.Background(), trees, passed, "r", c, b)
	if err != nil {
		t.Fatal(err)
	}
	// A package's own change runs it and its importer; a file a package lists runs that package; the rest are the base's.
	if m := got.Modules[0]; strings.Join(m.RunUnits, " ") != "svc/a svc/b svc/c" || strings.Join(m.SameUnits, " ") != "svc/d" {
		t.Errorf("svc: run %v same %v", m.RunUnits, m.SameUnits)
	}
	unitsContract(t, "scope.json", got)
	// An unreadable base: every unit runs, with the reason.
	whole, err := unitplan.ScopeOf(context.Background(), scopeTrees(t, map[string]map[string]string{b: {"x": "1"}, c: head}), passed, "r", c, b)
	if err != nil || whole.Note == "" || strings.Join(whole.Modules[0].RunUnits, " ") != "svc/a svc/b svc/c svc/d" || len(whole.Modules[0].SameUnits) != 0 {
		t.Fatalf("whole: %+v %v", whole, err)
	}
	unitsContract(t, "scope-whole.json", whole)
}
