package ciconfig

import (
	"encoding/json"
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
	"sort"
	"strings"
)

type frontendDeclaration struct {
	Root      string               `json:"root"`
	Inventory string               `json:"inventory"`
	Groups    map[string]Ownership `json:"groups"`
}
type frontendInventory struct {
	Version int             `json:"version"`
	Root    string          `json:"root"`
	Groups  []FrontendGroup `json:"groups"`
}
type FrontendGroup struct {
	SourceInputs      []string `json:"source_inputs"`
	TestInputs        []string `json:"test_inputs"`
	Path              string   `json:"path"`
	Files             []string `json:"files"`
	Imports           []string `json:"imports"`
	TestImports       []string `json:"test_imports"`
	ExternalImports   []string `json:"external_imports"`
	UnresolvedImports []string `json:"unresolved_imports"`
	Ownership
}
type FrontendView struct {
	Root              string          `json:"root"`
	Module            string          `json:"module"`
	Source            string          `json:"source"`
	Groups            []FrontendGroup `json:"groups"`
	Tasks             []string        `json:"tasks"`
	Inputs            []string        `json:"inputs"`
	Files             int             `json:"files"`
	Unresolved        int             `json:"unresolved"`
	InventoryComplete bool            `json:"inventory_complete"`
}

// Frontend declarations describe ownership, never create independent CI units.
// The inventory is generated with TypeScript's parser and compared in console tests.
func readFrontend(t *unitplan.Tree, manifest *unitplan.Manifest, plan *unitplan.Plan, d *frontendDeclaration, check func(Ownership) error) (*FrontendView, error) {
	raw, err := t.ReadFile(d.Inventory)
	if err != nil {
		return nil, fmt.Errorf("frontend inventory unreadable: %w", err)
	}
	var inv frontendInventory
	if err = json.Unmarshal(raw, &inv); err != nil {
		return nil, fmt.Errorf("frontend inventory unreadable: %w", err)
	}
	if inv.Version != 1 || inv.Root != d.Root || d.Root == "" {
		return nil, fmt.Errorf("frontend inventory root/version mismatch")
	}
	out := &FrontendView{Root: d.Root, Source: d.Inventory, Groups: []FrontendGroup{}, Tasks: []string{}, Inputs: []string{}}
	for _, m := range manifest.Modules {
		if d.Root == m.Root || strings.HasPrefix(d.Root, m.Root+"/") {
			if m.Kind == "go" || out.Module != "" {
				return nil, fmt.Errorf("frontend root must belong to one non-Go module")
			}
			out.Module = m.Name
			out.Inputs = append(out.Inputs, m.Needs...)
		}
	}
	if out.Module == "" {
		return nil, fmt.Errorf("frontend root has no CI module")
	}
	for _, u := range plan.Units {
		if u.Module == out.Module {
			out.Tasks = append(out.Tasks, u.Task)
		}
	}
	files := map[string]bool{}
	seen := map[string]bool{}
	for _, g := range inv.Groups {
		o, ok := d.Groups[g.Path]
		if !ok || seen[g.Path] {
			return nil, fmt.Errorf("frontend group missing or duplicate: %s", g.Path)
		}
		if err := check(o); err != nil {
			return nil, err
		}
		seen[g.Path] = true
		g.Ownership = o
		if o.Status != "assigned" {
			out.Unresolved++
		}
		if len(g.Files) == 0 {
			return nil, fmt.Errorf("empty frontend group: %s", g.Path)
		}
		for _, file := range g.Files {
			if files[file] || !strings.HasPrefix(file, d.Root+"/") {
				return nil, fmt.Errorf("frontend file outside root or duplicate: %s", file)
			}
			e, ok := t.Lookup(file)
			if !ok || e.Dir {
				return nil, fmt.Errorf("frontend inventory names missing file: %s", file)
			}
			owner := ""
			for p := range d.Groups {
				if (file == p || strings.HasPrefix(file, p+"/")) && len(p) > len(owner) {
					owner = p
				}
			}
			if owner != g.Path {
				return nil, fmt.Errorf("frontend file attributed to wrong source group: %s", file)
			}
			files[file] = true
		}
		for _, target := range append(append([]string{}, g.Imports...), g.TestImports...) {
			if _, ok := d.Groups[target]; !ok {
				return nil, fmt.Errorf("frontend import names missing group: %s", target)
			}
		}
		out.Groups = append(out.Groups, g)
	}
	for p := range d.Groups {
		if !seen[p] {
			return nil, fmt.Errorf("frontend declaration has no inventory group: %s", p)
		}
	}
	for _, e := range t.Walk(d.Root) {
		if !e.Dir && !files[e.Path] {
			return nil, fmt.Errorf("frontend inventory omits source file: %s", e.Path)
		}
	}
	out.Files = len(files)
	out.InventoryComplete = true
	sort.Strings(out.Tasks)
	sort.Slice(out.Groups, func(i, j int) bool { return out.Groups[i].Path < out.Groups[j].Path })
	return out, nil
}
