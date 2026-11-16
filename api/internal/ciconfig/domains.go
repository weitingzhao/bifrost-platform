package ciconfig

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

const OwnershipPath = "config/domain-ownership.json"

type Domain struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Source string `json:"source"`
}
type Ownership struct {
	Domains []string `json:"domains"`
	Status  string   `json:"status"`
	Basis   string   `json:"basis"`
}
type ownershipFile struct {
	Version  int                  `json:"version"`
	Source   string               `json:"source"`
	Domains  []Domain             `json:"domains"`
	Default  Ownership            `json:"default"`
	Packages map[string]Ownership `json:"packages"`
}
type Package struct {
	Path   string `json:"path"`
	Module string `json:"module"`
	Ownership
	Imports     []string `json:"imports"`
	TestImports []string `json:"test_imports"`
	Tasks       []string `json:"tasks"`
	Inputs      []string `json:"inputs"`
}
type SharedTask struct {
	Unit   string `json:"unit"`
	Task   string `json:"task"`
	Module string `json:"module"`
}

type DomainView struct {
	SharedTasks []SharedTask `json:"shared_tasks"`
	Source      string       `json:"source"`
	Domains     []Domain     `json:"domains"`
	Packages    []Package    `json:"packages"`
	// InventoryComplete means every Go package of the declared modules was
	// enumerated, not that business ownership or isolation is complete.
	InventoryComplete bool `json:"inventory_complete"`
	Unresolved        int  `json:"unresolved"`
}

// readDomains adds descriptive ownership to the exact tree already used for
// configuration. The executable manifest, input keys and decisions are untouched.
func readDomains(t *unitplan.Tree, manifest *unitplan.Manifest, plan *unitplan.Plan) (*DomainView, error) {
	if _, ok := t.Lookup(OwnershipPath); !ok {
		return nil, nil
	}
	raw, err := t.ReadFile(OwnershipPath)
	if err != nil {
		return nil, err
	}
	var doc ownershipFile
	if err = json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("domain ownership unreadable: %w", err)
	}
	if doc.Version != 1 {
		return nil, fmt.Errorf("domain ownership version %d is unsupported", doc.Version)
	}
	known := map[string]bool{}
	for _, d := range doc.Domains {
		if d.ID == "" || d.Name == "" || known[d.ID] {
			return nil, fmt.Errorf("domain ownership has missing or duplicate identity")
		}
		known[d.ID] = true
	}
	check := func(o Ownership) error {
		if len(o.Domains) == 0 || o.Basis == "" {
			return fmt.Errorf("domain ownership lacks scope or basis")
		}
		switch o.Status {
		case "assigned", "mixed", "provisional", "default-maintainer":
		default:
			return fmt.Errorf("domain ownership status %q is unknown", o.Status)
		}
		for _, id := range o.Domains {
			if !known[id] {
				return fmt.Errorf("unknown domain %q", id)
			}
		}
		return nil
	}
	if err = check(doc.Default); err != nil {
		return nil, err
	}
	for _, o := range doc.Packages {
		if err = check(o); err != nil {
			return nil, err
		}
	}
	out := &DomainView{Source: doc.Source, Domains: doc.Domains, Packages: []Package{}, InventoryComplete: true, SharedTasks: []SharedTask{}}
	seen := map[string]bool{}
	for _, mod := range manifest.Modules {
		if mod.Kind != "go" {
			continue
		}
		gm, err := unitplan.ReadGoModule(t, mod.Root)
		if err != nil {
			return nil, err
		}
		dirs := func(imports []string) []string {
			out := []string{}
			for _, ip := range imports {
				if p := gm.Packages[ip]; p != nil {
					out = append(out, p.Dir)
				}
			}
			sort.Strings(out)
			return out
		}
		for _, p := range gm.Packages {
			owner, ok := doc.Packages[p.Dir]
			if !ok {
				owner = doc.Default
			}
			seen[p.Dir] = true
			row := Package{Path: p.Dir, Module: mod.Name, Ownership: owner, Imports: dirs(p.Imports), TestImports: dirs(p.TestImports), Tasks: []string{}, Inputs: append([]string{}, mod.Needs...)}
			row.Inputs = append(row.Inputs, mod.Inputs[p.Dir]...)
			for _, u := range plan.Units {
				if u.Module == mod.Name && u.Unit == p.Dir {
					row.Tasks = append(row.Tasks, u.Task)
				}
			}
			sort.Strings(row.Tasks)
			if owner.Status != "assigned" {
				out.Unresolved++
			}
			out.Packages = append(out.Packages, row)
		}
	}
	for _, u := range plan.Units {
		if !seen[u.Unit] {
			out.SharedTasks = append(out.SharedTasks, SharedTask{Unit: u.Unit, Task: u.Task, Module: u.Module})
		}
	}
	sort.Slice(out.SharedTasks, func(i, j int) bool {
		a, b := out.SharedTasks[i], out.SharedTasks[j]
		return a.Unit+":"+a.Task < b.Unit+":"+b.Task
	})
	for p := range doc.Packages {
		if !seen[p] {
			return nil, fmt.Errorf("domain ownership names missing Go package %s", p)
		}
	}
	sort.Slice(out.Packages, func(i, j int) bool { return out.Packages[i].Path < out.Packages[j].Path })
	return out, nil
}
