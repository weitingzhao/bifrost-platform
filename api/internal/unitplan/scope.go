package unitplan

import (
	"fmt"
	"sort"
)

// Scope is which modules of a commit CI has to verify again, against another
// commit of the same repository (declaration standard §2): a module whose
// tasks all have the keys they had at the base is not run, and the base's
// results stand for it. It is computed from the two plans alone, so from the
// two declarations: nothing here names a module, a directory or a kind.
type Scope struct {
	Repo   string `json:"repo"`
	Commit string `json:"commit"`
	Base   string `json:"base"`
	// Declared is false when the commit has no declaration: the project is
	// then one block and all of it runs.
	Declared bool          `json:"declared"`
	Modules  []ModuleScope `json:"modules"`
	// Note says why everything runs when it is not the keys that say so.
	Note string `json:"note,omitempty"`
	// BaseRun is the CI run whose pass stands for the modules that do not
	// run: the base's, as the caller read it from the evidence ledger.
	BaseRun string `json:"base_run,omitempty"`
}

// ModuleScope is one module of the declaration.
type ModuleScope struct {
	Module  string `json:"module"`
	Tasks   int    `json:"tasks"`   // its tasks in the commit's plan
	Changed int    `json:"changed"` // those whose key the base's plan does not have
	Run     bool   `json:"run"`
	// Why names what differs, most tasks first: an input by its reference
	// ("needs other-repo:path"), or a part of the key ("tree", "libs",
	// "script"); at most whyLimit of them.
	Why []string `json:"why"`
	// RunUnits and SameUnits are the module's units by name, sorted: a unit
	// runs when one of its tasks has a key the base's plan does not have, and
	// is the same when all of them keep the base's keys. Their lengths are
	// the counts. Module-level Run is what CI acts on; these say what a run
	// by unit would be.
	RunUnits  []string `json:"run_units"`
	SameUnits []string `json:"same_units"`
}

const whyLimit = 5

// WholeProject is the scope of a commit that cannot be split: no declaration,
// or a base that cannot be compared with. Everything runs.
func WholeProject(repo, commit, base, note string, declared bool, head *Plan) Scope {
	s := Scope{Repo: repo, Commit: commit, Base: base, Declared: declared, Note: note, Modules: []ModuleScope{}}
	if head == nil {
		return s
	}
	count := map[string]int{}
	units := map[string]map[string]bool{}
	var order []string
	for _, u := range head.Units {
		if count[u.Module] == 0 {
			order = append(order, u.Module)
			units[u.Module] = map[string]bool{}
		}
		count[u.Module]++
		units[u.Module][u.Unit] = true
	}
	for _, m := range order {
		s.Modules = append(s.Modules, ModuleScope{Module: m, Tasks: count[m], Changed: count[m], Run: true, Why: []string{note},
			RunUnits: sortedKeys(units[m]), SameUnits: []string{}})
	}
	return s
}

// Compare is the scope of head against base, two plans of one repository.
func Compare(base, head *Plan) Scope {
	s := Scope{Repo: head.Repo, Commit: head.Commit, Base: base.Commit, Declared: true, Modules: []ModuleScope{}}
	was := map[string]PlannedTask{}
	for _, u := range base.Units {
		was[u.Unit+"\x00"+u.Task] = u
	}
	type tally struct {
		scope   ModuleScope
		reasons map[string]int
		units   map[string]bool // unit -> one of its tasks changed
	}
	byModule := map[string]*tally{}
	var order []string
	for _, u := range head.Units {
		t := byModule[u.Module]
		if t == nil {
			t = &tally{scope: ModuleScope{Module: u.Module, RunUnits: []string{}, SameUnits: []string{}}, reasons: map[string]int{}, units: map[string]bool{}}
			byModule[u.Module] = t
			order = append(order, u.Module)
		}
		t.scope.Tasks++
		if _, seen := t.units[u.Unit]; !seen {
			t.units[u.Unit] = false
		}
		old, had := was[u.Unit+"\x00"+u.Task]
		if had && old.Key == u.Key {
			continue
		}
		t.scope.Changed++
		t.units[u.Unit] = true
		if !had {
			t.reasons["new: the base has no such task"]++
			continue
		}
		for _, r := range differing(old.Inputs, u.Inputs) {
			t.reasons[r]++
		}
	}
	for _, m := range order {
		t := byModule[m]
		t.scope.Run = t.scope.Changed > 0
		for _, u := range sortedKeys(t.units) {
			if t.units[u] {
				t.scope.RunUnits = append(t.scope.RunUnits, u)
			} else {
				t.scope.SameUnits = append(t.scope.SameUnits, u)
			}
		}
		names := make([]string, 0, len(t.reasons))
		for r := range t.reasons {
			names = append(names, r)
		}
		sort.Slice(names, func(i, j int) bool {
			if t.reasons[names[i]] != t.reasons[names[j]] {
				return t.reasons[names[i]] > t.reasons[names[j]]
			}
			return names[i] < names[j]
		})
		t.scope.Why = []string{}
		for i, r := range names {
			if i == whyLimit {
				t.scope.Why = append(t.scope.Why, fmt.Sprintf("and %d more", len(names)-whyLimit))
				break
			}
			t.scope.Why = append(t.scope.Why, fmt.Sprintf("%s (%d of %d tasks)", r, t.reasons[r], t.scope.Tasks))
		}
		s.Modules = append(s.Modules, t.scope)
	}
	return s
}

// differing names the parts of a key document that are not the same in two
// of them. A list of input references ("needs") is named one reference at a
// time.
func differing(old, now map[string]any) []string {
	var out []string
	seen := map[string]bool{}
	for k := range old {
		seen[k] = true
	}
	for k := range now {
		seen[k] = true
	}
	for _, k := range sortedKeys(seen) {
		a, b := old[k], now[k]
		if Hash(a) == Hash(b) {
			continue
		}
		if k == "needs" {
			if refs := differingRefs(a, b); len(refs) > 0 {
				for _, r := range refs {
					out = append(out, "needs "+r)
				}
				continue
			}
		}
		out = append(out, k)
	}
	return out
}

func differingRefs(old, now any) []string {
	index := func(v any) map[string]string {
		out := map[string]string{}
		list, _ := v.([]any)
		for _, e := range list {
			doc, _ := e.(map[string]any)
			if ref, ok := doc["ref"].(string); ok {
				out[ref] = Hash(doc)
			}
		}
		return out
	}
	a, b := index(old), index(now)
	seen := map[string]bool{}
	for r := range a {
		seen[r] = true
	}
	for r := range b {
		seen[r] = true
	}
	var out []string
	for _, r := range sortedKeys(seen) {
		if a[r] != b[r] {
			out = append(out, r)
		}
	}
	return out
}
