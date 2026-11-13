package unitplan

import (
	"fmt"
	"strings"
)

// Plan is what one commit's CI consists of: the commit set (the commit and
// its pins) and every unit's task with its key.
type Plan struct {
	Repo   string        `json:"repo"`
	Commit string        `json:"commit"`
	Tree   string        `json:"tree"`
	Pins   []PlanPin     `json:"pins"`
	Units  []PlannedTask `json:"units"`
}

// PlanPin is a pinned repository at its commit, with that commit's root tree.
type PlanPin struct {
	Repo   string `json:"repo"`
	Commit string `json:"commit"`
	Tree   string `json:"tree"`
}

// PlannedTask is one task of one unit: its key and the document the key is
// the hash of (the ledger's inputs column, "v": 2).
type PlannedTask struct {
	Unit   string         `json:"unit"`
	Task   string         `json:"task"`
	Module string         `json:"module"`
	Key    string         `json:"input_key"`
	Inputs map[string]any `json:"inputs"`
	Shards int            `json:"shards,omitempty"`
}

// Source reads the trees a plan needs: Pinned is the tree of another
// repository at a commit.
type Source struct {
	Pinned func(repo, commit string) (*Tree, error)
}

// entryScripts is the script a task runs, from the repository root (landing
// stage 2 §4.0). Its blob id is in the key: a changed script is a changed
// task. A script the tree does not have yet has the id "".
var entryScripts = map[string]string{
	"go/test": "scripts/ci/unit-go-test", "go/vet": "scripts/ci/unit-go-vet",
	"node/typecheck": "scripts/ci/unit-console-typecheck", "node/lint": "scripts/ci/unit-console-lint",
	"node/test": "scripts/ci/unit-console-test", "node/build": "scripts/ci/unit-console-build",
	"/check": "scripts/ci/unit-check",
}

// sharedScripts is what the entry scripts of a kind run beside themselves:
// the four scripts of a node unit are one line each and exec unit-node, so
// its blob id is in their keys too.
var sharedScripts = map[string][]string{"node": {"scripts/ci/unit-node"}}

// Build computes the plan of a commit from its tree and the manifest in it.
func Build(repo, commit string, t *Tree, src Source) (*Plan, error) {
	doc, err := t.ReadFile(ManifestPath)
	if err != nil {
		return nil, fmt.Errorf("unitplan: %s@%s has no %s: %w", repo, commit, ManifestPath, err)
	}
	return buildPlan(repo, commit, t, src, doc, true)
}

// BuildWith computes the plan of a commit with a manifest given beside it: a
// commit from before the repository had one can be planned as if it had.
// The manifest may then name a package the commit does not have yet.
func BuildWith(repo, commit string, t *Tree, src Source, doc []byte) (*Plan, error) {
	return buildPlan(repo, commit, t, src, doc, false)
}

func buildPlan(repo, commit string, t *Tree, src Source, doc []byte, own bool) (*Plan, error) {
	m, err := ParseManifest(doc)
	if err != nil {
		return nil, err
	}
	plan := &Plan{Repo: repo, Commit: commit, Tree: t.RootID, Pins: []PlanPin{}, Units: []PlannedTask{}}
	pinned := map[string]*Tree{}
	for _, name := range sortedKeys(m.Pins) {
		if src.Pinned == nil {
			return nil, fmt.Errorf("unitplan: %s pins %s and no source reads it", repo, name)
		}
		pt, err := src.Pinned(name, m.Pins[name].Commit)
		if err != nil {
			return nil, fmt.Errorf("unitplan: pin %s@%s: %w", name, m.Pins[name].Commit, err)
		}
		pinned[name] = pt
		plan.Pins = append(plan.Pins, PlanPin{Repo: name, Commit: m.Pins[name].Commit, Tree: pt.RootID})
	}
	// ref resolves one declared input to the id of what it names: "." is the
	// whole repository and every pin; "repo:path" a path of a pinned
	// repository at its pin; anything else a path of this repository.
	ref := func(r string) (map[string]any, error) {
		if r == "." {
			all := map[string]any{"ref": ".", "tree": t.RootID}
			for _, p := range plan.Pins {
				all["pin:"+p.Repo] = p.Tree
			}
			return all, nil
		}
		from, p := t, r
		if name, rest, other := strings.Cut(r, ":"); other {
			from, p = pinned[name], rest
		}
		e, ok := from.Lookup(p)
		if !ok {
			return nil, fmt.Errorf("unitplan: input %q names nothing in the tree it is read from", r)
		}
		return map[string]any{"ref": r, "id": e.ID}, nil
	}
	refs := func(lists ...[]string) ([]any, error) {
		seen := map[string]bool{}
		out := []any{}
		for _, list := range lists {
			for _, r := range list {
				if seen[r] {
					continue
				}
				seen[r] = true
				doc, err := ref(r)
				if err != nil {
					return nil, err
				}
				out = append(out, doc)
			}
		}
		return out, nil
	}
	scriptID := func(kind, task string) map[string]any {
		path := entryScripts[kind+"/"+task]
		if path == "" {
			path = entryScripts["/"+task]
		}
		e, _ := t.Lookup(path)
		out := map[string]any{"path": path, "id": e.ID}
		if shared := sharedScripts[kind]; len(shared) > 0 {
			with := make([]any, 0, len(shared))
			for _, p := range shared {
				se, _ := t.Lookup(p)
				with = append(with, map[string]any{"path": p, "id": se.ID})
			}
			out["with"] = with
		}
		return out
	}
	add := func(mod Module, unit, task string, shards int, parts map[string]any) error {
		if !unitName.MatchString(unit) {
			return fmt.Errorf("unitplan: %q is not a unit name the ledger takes", unit)
		}
		inputs := map[string]any{"v": 2, "repo": repo, "unit": unit, "task": task,
			"image": mod.Image, "template": mod.Template, "db_roles": mod.DBRoles, "script": scriptID(mod.Kind, task), "shards": shards}
		for k, v := range parts {
			inputs[k] = v
		}
		plan.Units = append(plan.Units, PlannedTask{Unit: unit, Task: task, Module: mod.Name, Key: Hash(inputs), Inputs: inputs, Shards: shards})
		return nil
	}
	for _, mod := range m.Modules {
		root := strings.Trim(mod.Root, "/")
		if root == "." {
			root = ""
		}
		if mod.Kind != "go" {
			unit := root
			if unit == "" {
				unit = "repo"
			}
			own, ok := t.Lookup(root)
			if !ok {
				return nil, fmt.Errorf("unitplan: module %s: the tree has no %q", mod.Name, mod.Root)
			}
			for _, task := range sortedKeys(mod.Tasks) {
				needs, err := refs(mod.Needs, mod.Tasks[task].Needs)
				if err != nil {
					return nil, err
				}
				if err = add(mod, unit, task, mod.Tasks[task].Shards, map[string]any{"tree": own.ID, "needs": needs}); err != nil {
					return nil, err
				}
			}
			continue
		}
		gm, err := ReadGoModule(t, root)
		if err != nil {
			return nil, err
		}
		for _, task := range sortedKeys(mod.Tasks) {
			if mod.Tasks[task].Per == "module" {
				// The module's task (vet) reads every package whole: its
				// own files (lib, with what it imports) and its test files.
				libs, tests := []string{}, []string{}
				for _, ip := range gm.sorted() {
					libs = append(libs, ip+"="+gm.Packages[ip].Lib)
					if pkg := gm.Packages[ip]; len(pkg.TestFiles) > 0 {
						tests = append(tests, ip+"="+Hash(pkg.TestFiles))
					}
				}
				needs, err := refs(mod.Needs, mod.Tasks[task].Needs)
				if err != nil {
					return nil, err
				}
				if err = add(mod, root, task, 0, map[string]any{"libs": libs, "tests": tests, "go_mod": gm.GoMod, "go_sum": gm.GoSum, "needs": needs}); err != nil {
					return nil, err
				}
				continue
			}
			for _, ip := range gm.sorted() {
				pkg := gm.Packages[ip]
				if !pkg.HasTests {
					continue
				}
				parts, err := gm.TestKeyParts(ip)
				if err != nil {
					return nil, err
				}
				if parts["needs"], err = refs(mod.Needs, mod.Tasks[task].Needs, mod.Inputs[pkg.Dir]); err != nil {
					return nil, fmt.Errorf("%w (package %s)", err, pkg.Dir)
				}
				if err = add(mod, pkg.Dir, task, mod.Shards[pkg.Dir], parts); err != nil {
					return nil, err
				}
			}
		}
		for dir := range mod.Inputs {
			if !own {
				break
			}
			if p, ok := gm.Packages[gm.Path+strings.TrimPrefix(dir, root)]; !ok || !p.HasTests {
				return nil, fmt.Errorf("unitplan: module %s declares inputs for %s, which is not a package with tests", mod.Name, dir)
			}
		}
	}
	return plan, nil
}
