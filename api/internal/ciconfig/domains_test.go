package ciconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestDomainInventoryAndDependencies(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"bifrost.units.yaml": "version: 1\nmodules:\n  - name: api\n    kind: go\n    root: api\n    tasks: {test: {per: package}, vet: {per: module}}\n",
		"api/go.mod":         "module example.com/m\n\ngo 1.25\n",
		"api/a/a.go":         "package a\nimport _ \"example.com/m/b\"\n", "api/a/a_test.go": "package a\n",
		"api/b/b.go": "package b\n", "api/c/c.go": "package c\n",
	}
	doc := ownershipFile{Version: 1, Domains: []Domain{{ID: "plan", Name: "Planning"}, {ID: "foundation", Name: "Foundation"}}, Default: Ownership{Domains: []string{"foundation"}, Status: "default-maintainer", Basis: "default ruling"}, Packages: map[string]Ownership{"api/a": {Domains: []string{"plan"}, Status: "assigned", Basis: "charter"}}}
	raw, _ := json.Marshal(doc)
	files[OwnershipPath] = string(raw)
	trees := scopeTrees(t, map[string]map[string]string{"c": files})
	got, err := Read(context.Background(), trees, "r", "c")
	if err != nil || got.DomainNote != "" || got.Domains == nil {
		t.Fatalf("%+v %v", got, err)
	}
	v := got.Domains
	if !v.InventoryComplete || len(v.Packages) != 3 || v.Unresolved != 2 {
		t.Fatalf("%+v", v)
	}
	if p := v.Packages[0]; p.Path != "api/a" || fmt.Sprint(p.Imports) != "[api/b]" || fmt.Sprint(p.Tasks) != "[test]" {
		t.Fatalf("%+v", p)
	}
	if len(v.SharedTasks) != 1 || v.SharedTasks[0].Task != "vet" {
		t.Fatalf("shared module tasks: %+v", v.SharedTasks)
	}
	if len(v.Packages[1].Tasks) != 0 {
		t.Fatal("package without tests must not invent a CI test task")
	}
	doc.Packages["api/missing"] = doc.Packages["api/a"]
	raw, _ = json.Marshal(doc)
	files[OwnershipPath] = string(raw)
	got, err = Read(context.Background(), trees, "r", "c")
	if err != nil || got.Domains != nil || !strings.Contains(got.DomainNote, "missing Go package") || len(got.Modules) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	files[OwnershipPath] = "{"
	got, err = Read(context.Background(), trees, "r", "c")
	if err != nil || got.Domains != nil || got.DomainNote == "" || len(got.Modules) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	delete(files, OwnershipPath)
	got, err = Read(context.Background(), trees, "r", "c")
	if err != nil || got.Domains != nil || got.DomainNote != "" {
		t.Fatalf("%+v %v", got, err)
	}
}
