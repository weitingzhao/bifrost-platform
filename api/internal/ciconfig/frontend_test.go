package ciconfig

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestFrontendInventorySharesRealTasksAndKeepsGoOnStaleInventory(t *testing.T) {
	files := map[string]string{
		"bifrost.units.yaml": "version: 1\nmodules:\n  - name: web\n    root: web\n    needs: [\"config/domain-ownership.json\"]\n    tasks: {test: {}, build: {}}\n",
		"web/src/a.ts":       "import './b/b'", "web/src/b/b.ts": "export const b = 1",
	}
	own := Ownership{Domains: []string{"plan"}, Status: "assigned", Basis: "source audit"}
	d := ownershipFile{Version: 1, Domains: []Domain{{ID: "plan", Name: "Planning"}}, Default: own, Frontend: &frontendDeclaration{Root: "web/src", Inventory: "web/inventory.json", Groups: map[string]Ownership{"web/src": own, "web/src/b": own}}}
	raw, _ := json.Marshal(d)
	files[OwnershipPath] = string(raw)
	inv := frontendInventory{Version: 1, Root: "web/src", Groups: []FrontendGroup{{Path: "web/src", Files: []string{"web/src/a.ts"}, Imports: []string{"web/src/b"}}, {Path: "web/src/b", Files: []string{"web/src/b/b.ts"}}}}
	raw, _ = json.Marshal(inv)
	files["web/inventory.json"] = string(raw)
	read := func() Answer {
		v, err := Read(context.Background(), scopeTrees(t, map[string]map[string]string{"c": files}), "r", "c")
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	got := read()
	if got.Domains == nil || got.Domains.Frontend == nil {
		t.Fatalf("%+v", got)
	}
	f := got.Domains.Frontend
	if f.Files != 2 || !f.InventoryComplete || f.Module != "web" || len(f.Tasks) != 2 || len(got.Domains.SharedTasks) != 2 || f.Groups[0].Imports[0] != "web/src/b" {
		t.Fatalf("%+v", f)
	}
	files["web/src/new.ts"] = "export const added=1"
	got = read()
	if got.Domains == nil || got.Domains.Frontend != nil || !strings.Contains(got.Domains.FrontendNote, "omits source file") || len(got.Modules) != 1 {
		t.Fatalf("stale inventory must not change execution config: %+v", got)
	}
	delete(files, "web/src/new.ts")
	delete(files, "web/src/b/b.ts")
	got = read()
	if got.Domains == nil || !strings.Contains(got.Domains.FrontendNote, "missing file") {
		t.Fatalf("deleted file: %+v", got)
	}
}
