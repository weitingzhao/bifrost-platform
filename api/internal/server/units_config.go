package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

// GET /api/v1/units/config?repo=…[&commit=…] is how CI is set up for one
// project, read from the declaration CI itself reads (declaration standard
// §6, the configuration view): whether the project has one, its modules, what
// each reads, and how many units and tasks that makes. Without commit it is
// main's head. A project with no declaration answers declared=false: it is
// one block and all of it runs every time. Nothing here is written by hand
// for the page; a module the declaration does not name is not in the answer.
// Read-only; viewer and above, as /units/plan.

type unitConfigTask struct {
	Task   string `json:"task"`
	Units  int    `json:"units"`
	Shards int    `json:"shards,omitempty"` // the most any unit of this task is cut into
}

type unitConfigModule struct {
	Name     string           `json:"name"`
	Kind     string           `json:"kind,omitempty"`
	Root     string           `json:"root"`
	Image    string           `json:"image,omitempty"`
	Template string           `json:"template,omitempty"`
	Needs    []string         `json:"needs"` // inputs of every task, beside the module's own tree
	Tasks    []unitConfigTask `json:"tasks"`
	Units    int              `json:"units"`
	// DeclaredInputs counts the units that declare inputs of their own;
	// WholeRepository those among them (or the module itself, through needs)
	// whose input is "." — they run on every change, whatever it touches.
	DeclaredInputs  int `json:"declared_inputs"`
	WholeRepository int `json:"whole_repository"`
}

type unitConfigAnswer struct {
	Repo     string             `json:"repo"`
	Commit   string             `json:"commit"`
	Declared bool               `json:"declared"`
	Note     string             `json:"note,omitempty"`
	Pins     []unitplan.PlanPin `json:"pins"`
	Modules  []unitConfigModule `json:"modules"`
}

func unitConfig(ctx context.Context, trees unitTreesFunc, repo, commit string) (unitConfigAnswer, error) {
	out := unitConfigAnswer{Repo: repo, Commit: commit, Pins: []unitplan.PlanPin{}, Modules: []unitConfigModule{}}
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
	out.Pins = plan.Pins
	for _, mod := range manifest.Modules {
		m := unitConfigModule{Name: mod.Name, Kind: mod.Kind, Root: mod.Root, Image: mod.Image, Template: mod.Template,
			Needs: append([]string{}, mod.Needs...), Tasks: []unitConfigTask{}, DeclaredInputs: len(mod.Inputs)}
		byTask, units := map[string]*unitConfigTask{}, map[string]bool{}
		for _, u := range plan.Units {
			if u.Module != mod.Name {
				continue
			}
			units[u.Unit] = true
			task := byTask[u.Task]
			if task == nil {
				task = &unitConfigTask{Task: u.Task}
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

func (s *Server) handleUnitConfig(w http.ResponseWriter, r *http.Request) {
	repo, commit := strings.TrimSpace(r.URL.Query().Get("repo")), strings.TrimSpace(r.URL.Query().Get("commit"))
	if !giteaName.MatchString(repo) || strings.HasSuffix(repo, ".git") || (commit != "" && !commitID.MatchString(commit)) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo is a repository name; commit, when given, a 40-character lowercase SHA"})
		return
	}
	if s.delivery == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "this platform does not read Gitea"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	trees := s.unitTreesOf()
	if commit == "" {
		body, err := s.unitTrees.get(ctx, url.PathEscape(repo)+"/branches/main", 1<<20)
		var branch struct {
			Commit struct {
				ID string `json:"id"`
			} `json:"commit"`
		}
		if err == nil {
			err = json.Unmarshal(body, &branch)
		}
		if err != nil || !commitID.MatchString(branch.Commit.ID) {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "gitea: main of " + repo + " could not be read"})
			return
		}
		commit = branch.Commit.ID
	}
	out, err := unitConfig(ctx, trees, repo, commit)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}
