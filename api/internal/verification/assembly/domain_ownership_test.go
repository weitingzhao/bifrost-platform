// Moved from server (domain_ownership_test.go) on 2026-11-23, with UnitConfig.

package assembly

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Keep the source-owned directory attached to real packages. The production
// projection reads real imports from the requested commit, not these stubs.
func TestDomainOwnershipCatalog(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..", "..")
	raw, err := os.ReadFile(filepath.Join(root, "config", "domain-ownership.json"))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"config/domain-ownership.json": string(raw),
		"bifrost.units.yaml":           "version: 1\nmodules:\n  - name: api\n    kind: go\n    root: api\n    tasks: {test: {per: package}, vet: {per: module}}\n",
		"api/go.mod":                   "module example.com/catalog\n\ngo 1.25\n",
	}
	packages := map[string]bool{}
	err = filepath.WalkDir(filepath.Join(root, "api"), func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			if e.Name() == "testdata" || strings.HasPrefix(e.Name(), ".") || strings.HasPrefix(e.Name(), "_") || e.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		dir := filepath.ToSlash(rel)
		packages[dir] = true
		files[dir+"/catalog.go"] = "package catalog\n"
		if strings.HasSuffix(path, "_test.go") {
			files[dir+"/catalog_test.go"] = "package catalog\n"
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnitConfig(context.Background(), scopeTrees(t, map[string]map[string]string{"c": files}), "r", "c")
	if err != nil || got.Domains == nil || got.DomainNote != "" {
		t.Fatalf("catalog: %+v %v", got, err)
	}
	if len(got.Domains.Packages) != len(packages) || !got.Domains.InventoryComplete {
		t.Fatalf("inventory: %d packages, want %d", len(got.Domains.Packages), len(packages))
	}
	// Every package the table does not give to one domain stays visible as
	// unresolved, and no more. Origin: 2026-11-21, law 95 — the last three
	// mixed packages became the assembly domain and the count reached 0; the
	// check used to be "is not 0", which that would have failed for a truth.
	var table struct {
		Default  struct{ Status string }
		Packages map[string]struct{ Status string }
	}
	if err := json.Unmarshal(raw, &table); err != nil {
		t.Fatal(err)
	}
	want := 0
	for dir := range packages {
		row, ok := table.Packages[dir]
		if !ok {
			row = table.Default
		}
		if row.Status != "assigned" {
			want++
		}
	}
	if got.Domains.Unresolved != want {
		t.Fatalf("unresolved = %d, want %d (packages whose row is not assigned)", got.Domains.Unresolved, want)
	}
}
