// Package ciconfig projects a commit's CI declarations for readers. It does
// not select tests, authorize a merge or authorize a release.
package ciconfig

import (
	"context"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
	"sort"
)

type Trees func(context.Context, string, string) (*unitplan.Tree, error)
type Task struct {
	Task   string `json:"task"`
	Units  int    `json:"units"`
	Shards int    `json:"shards,omitempty"` // the most any unit of this task is cut into
}

type Module struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind,omitempty"`
	Root     string   `json:"root"`
	Image    string   `json:"image,omitempty"`
	Template string   `json:"template,omitempty"`
	Needs    []string `json:"needs"` // inputs of every task, beside the module's own tree
	Tasks    []Task   `json:"tasks"`
	Units    int      `json:"units"`
	// DeclaredInputs counts the units that declare inputs of their own;
	// WholeRepository those among them (or the module itself, through needs)
	// whose input is "." — they run on every change, whatever it touches.
	DeclaredInputs  int `json:"declared_inputs"`
	WholeRepository int `json:"whole_repository"`
}

type Answer struct {
	Domains    *DomainView        `json:"domains,omitempty"`
	DomainNote string             `json:"domain_note,omitempty"`
	Repo       string             `json:"repo"`
	Commit     string             `json:"commit"`
	Declared   bool               `json:"declared"`
	Note       string             `json:"note,omitempty"`
	Pins       []unitplan.PlanPin `json:"pins"`
	Modules    []Module           `json:"modules"`
}

func Read(ctx context.Context, trees Trees, repo, commit string) (Answer, error) {
	out := Answer{Repo: repo, Commit: commit, Pins: []unitplan.PlanPin{}, Modules: []Module{}}
	t, err := trees(ctx, repo, commit)
	if err != nil {
		return out, err
	}
	doc, err := t.ReadFile(unitplan.ManifestPath)
	if err != nil {
		out.Note = "no " + unitplan.ManifestPath + ": the project is one block and all of it runs every time"
		return out, nil
	}
	out.Declared = true
	manifest, err := unitplan.ParseManifest(doc)
	if err != nil {
		out.Note = "the declaration does not read, so everything runs: " + err.Error()
		return out, nil
	}
	plan, err := unitplan.Build(repo, commit, t, unitplan.Source{Pinned: func(p, at string) (*unitplan.Tree, error) { return trees(ctx, p, at) }})
	if err != nil {
		out.Note = "the declaration does not read, so everything runs: " + err.Error()
		return out, nil
	}
	out.Domains, err = readDomains(t, manifest, plan)
	if err != nil {
		out.DomainNote = err.Error()
	}
	out.Pins = plan.Pins
	for _, mod := range manifest.Modules {
		m := Module{Name: mod.Name, Kind: mod.Kind, Root: mod.Root, Image: mod.Image, Template: mod.Template,
			Needs: append([]string{}, mod.Needs...), Tasks: []Task{}, DeclaredInputs: len(mod.Inputs)}
		byTask, units := map[string]*Task{}, map[string]bool{}
		for _, u := range plan.Units {
			if u.Module != mod.Name {
				continue
			}
			units[u.Unit] = true
			task := byTask[u.Task]
			if task == nil {
				task = &Task{Task: u.Task}
				byTask[u.Task] = task
			}
			task.Units++
			if u.Shards > task.Shards {
				task.Shards = u.Shards
			}
		}
		names := make([]string, 0, len(byTask))
		for name := range byTask {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			m.Tasks = append(m.Tasks, *byTask[name])
		}
		m.Units = len(units)
		whole := func(list []string) bool {
			for _, r := range list {
				if r == "." {
					return true
				}
			}
			return false
		}
		if whole(mod.Needs) {
			m.WholeRepository = m.Units
		} else {
			for _, list := range mod.Inputs {
				if whole(list) {
					m.WholeRepository++
				}
			}
		}
		out.Modules = append(out.Modules, m)
	}
	return out, nil
}
