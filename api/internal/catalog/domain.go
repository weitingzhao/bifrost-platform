package catalog

import (
	"encoding/json"
	"net/http"
	"path"
	"sort"
	"strings"
)

// One domain's whole page of the map (2026-11-20, law 50: the domains, what
// each owns and what it opens are in one small module): what it owns in the
// table's repository and in the cluster, its contract as written, and its
// lights. Everything comes from what the scan kept; nothing is listed at
// request time.

// Domain is the answer of GET /catalog/domain?id=….
type Domain struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
	Repo    string `json:"repo"`
	Commit  string `json:"commit"`
	// Owns: Go packages, console groups and other paths of the table's
	// repository whose row names the domain; Shared marks a row that names
	// more than one domain (law 50 wants one).
	Packages  []Row `json:"packages"`
	Groups    []Row `json:"groups"`
	Paths     []Row `json:"paths"`
	Workloads []Row `json:"workloads"`
	Tables    []Row `json:"tables"`
	// Buildings group its Go packages by the first directory under the Go
	// root and its internal directory (api/internal/bus/... is building bus,
	// api/cmd/x is cmd): computed from the paths, typed by nobody (2026-11-21,
	// City draws a district's buildings; law 30: nothing in Ops is kept by
	// hand). A domain that wants other buildings can declare them later; this
	// is the default.
	Buildings []Building `json:"buildings"`
	// BuildingsOver is how many buildings did not fit in MaxPerLayer and
	// were drawn as one "other" (law 79, 2026-11-22: more than 16 in a layer
	// is split too fine; it is shown, not merged for the domain).
	BuildingsOver int `json:"buildings_over"`
	// About and Tasks are the domain's own account of itself (law 77,
	// 2026-11-22: each domain explains itself; the standard is in the
	// content, read on demand through this route): one line on what it is,
	// and the tasks someone comes to it for, each with what to use (a route,
	// an MCP tool, an action). Written by the domain in its table row.
	About string `json:"about,omitempty"`
	Tasks []Task `json:"tasks"`
	// Contract is the domain's contracts block in the table, as written.
	Contract json.RawMessage `json:"contract"`
	Status   *DomainStatus   `json:"status,omitempty"`
	Note     string          `json:"note,omitempty"`
}

// Task is one thing someone comes to a domain to get done, and what to use.
type Task struct {
	Task string   `json:"task"`
	Use  []string `json:"use"`
	Note string   `json:"note,omitempty"`
}

// TaskMatch is a task found by what someone wants done.
type TaskMatch struct {
	Domain string   `json:"domain"`
	Task   string   `json:"task"`
	Use    []string `json:"use"`
	Note   string   `json:"note,omitempty"`
}

// domainRow is what this file reads of a domain's row.
type domainRow struct {
	ID        string          `json:"id"`
	About     string          `json:"about"`
	Tasks     []Task          `json:"tasks"`
	Contracts json.RawMessage `json:"contracts"`
}

// TasksFor finds the tasks whose words, or whose domain's about, hold every
// word of q (case does not matter); a domain that wrote no tasks is listed
// in Silent.
func (s *Scanner) TasksFor(q string) (matches []TaskMatch, silent []string) {
	matches, silent = []TaskMatch{}, []string{}
	k, ok := s.keptSections()
	if !ok {
		return
	}
	var rows []domainRow
	_ = json.Unmarshal(k.Domains, &rows)
	words := strings.Fields(strings.ToLower(q))
	for _, d := range rows {
		if len(d.Tasks) == 0 {
			silent = append(silent, d.ID)
		}
		for _, t := range d.Tasks {
			text := strings.ToLower(t.Task + " " + t.Note + " " + strings.Join(t.Use, " ") + " " + d.About)
			all := true
			for _, w := range words {
				if !strings.Contains(text, w) {
					all = false
					break
				}
			}
			if all {
				use := t.Use
				if use == nil {
					use = []string{}
				}
				matches = append(matches, TaskMatch{Domain: d.ID, Task: t.Task, Use: use, Note: t.Note})
			}
		}
	}
	return
}

// Building is a group of a domain's packages. Other marks the one that
// holds the buildings past MaxPerLayer.
type Building struct {
	ID       string   `json:"id"`
	Packages []string `json:"packages"`
	Other    bool     `json:"other,omitempty"`
}

// MaxPerLayer is how many things one layer of the map holds (law 79).
const MaxPerLayer = 16

// capBuildings keeps the largest MaxPerLayer-1 buildings and folds the rest
// into one "other"; it returns how many were folded.
func capBuildings(b []Building) ([]Building, int) {
	if len(b) <= MaxPerLayer {
		return b, 0
	}
	keep, rest := b[:MaxPerLayer-1], b[MaxPerLayer-1:]
	other := Building{ID: "other", Packages: []string{}, Other: true}
	for _, r := range rest {
		other.Packages = append(other.Packages, r.Packages...)
	}
	sort.Strings(other.Packages)
	return append(append([]Building{}, keep...), other), len(rest)
}

// buildingsOf groups packages by their first directory under a Go root and
// its "internal".
func buildingsOf(rows []Row, roots []string) []Building {
	isRoot := map[string]bool{}
	for _, r := range roots {
		isRoot[r] = true
	}
	by := map[string][]string{}
	var cmds []string
	for _, r := range rows {
		segs := strings.Split(r.Path, "/")
		if len(segs) > 1 && isRoot[segs[0]] {
			segs = segs[1:]
		}
		if len(segs) > 1 && segs[0] == "internal" {
			segs = segs[1:]
		}
		if len(segs) > 1 && segs[0] == "cmd" {
			cmds = append(cmds, r.Path)
			continue
		}
		by[segs[0]] = append(by[segs[0]], r.Path)
	}
	// A command goes in the building of the package it is the command of
	// (api/cmd/owner-exec with ownerexec, 2026-11-22 Approvals), when the
	// domain has that building; otherwise in cmd.
	for _, c := range cmds {
		name := strings.ReplaceAll(path.Base(c), "-", "")
		if _, ok := by[name]; ok {
			by[name] = append(by[name], c)
		} else {
			by["cmd"] = append(by["cmd"], c)
		}
	}
	out := make([]Building, 0, len(by))
	for id, pkgs := range by {
		sort.Strings(pkgs)
		out = append(out, Building{ID: id, Packages: pkgs})
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Packages) != len(out[j].Packages) {
			return len(out[i].Packages) > len(out[j].Packages)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Row is one row that names the domain.
type Row struct {
	Path   string   `json:"path"`
	Shared []string `json:"shared,omitempty"`
}

func rowsOf(id string, m map[string][]string) []Row {
	out := []Row{}
	for p, ds := range m {
		named, others := false, []string{}
		for _, d := range ds {
			if d == id {
				named = true
			} else {
				others = append(others, d)
			}
		}
		if !named {
			continue
		}
		r := Row{Path: p}
		if len(others) > 0 {
			sort.Strings(others)
			r.Shared = others
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// DomainOf assembles one domain from the kept table, owners and status.
func (s *Scanner) DomainOf(id string) (Domain, bool) {
	out := Domain{Version: Version, ID: id, Packages: []Row{}, Groups: []Row{}, Paths: []Row{}, Workloads: []Row{}, Tables: []Row{}, Buildings: []Building{}, Tasks: []Task{}}
	k, ok := s.keptSections()
	if !ok {
		out.Note = "no scanned table names its domains yet"
		return out, true
	}
	var domains []json.RawMessage
	_ = json.Unmarshal(k.Domains, &domains)
	found := false
	for _, raw := range domains {
		var d domainRow
		if json.Unmarshal(raw, &d) == nil && d.ID == id {
			found, out.Contract, out.About = true, d.Contracts, d.About
			if d.Tasks != nil {
				out.Tasks = d.Tasks
			}
		}
	}
	if !found {
		return out, false
	}
	out.Repo, out.Commit = k.Repo, k.Commit
	if o, _ := s.lastOwners(k.Repo); o != nil {
		out.Packages, out.Groups, out.Paths = rowsOf(id, o.Packages), rowsOf(id, o.Groups), rowsOf(id, o.Paths)
		out.Buildings, out.BuildingsOver = capBuildings(buildingsOf(out.Packages, o.GoRoots))
	}
	if o, _ := s.lastOwners(ClusterRepo); o != nil {
		out.Workloads = rowsOf(id, o.Paths)
	}
	if o, _ := s.lastOwners(DatabaseRepo); o != nil {
		out.Tables = rowsOf(id, o.Paths)
	}
	if raw, err := s.kept().Read(keptStatus); err == nil && raw != nil {
		var st Status
		if json.Unmarshal(raw, &st) == nil {
			for _, d := range st.Domains {
				if d.ID == id {
					d := d
					out.Status = &d
				}
			}
		}
	}
	if out.Status == nil {
		out.Note = "its lights are not computed yet"
	}
	return out, true
}

// DomainRoute is GET /catalog/domain?id=….
type DomainRoute struct{ Scanner *Scanner }

func (rt DomainRoute) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if q := strings.TrimSpace(r.URL.Query().Get("task")); id == "" && q != "" {
		if len(q) > 200 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "task is a few words of what you want done, at most 200 characters"})
			return
		}
		m, silent := rt.Scanner.TasksFor(q)
		writeJSON(w, http.StatusOK, map[string]any{"version": Version, "task": q, "matches": m, "domains_without_tasks": silent})
		return
	}
	if id == "" || len(id) > 64 || !repoName.MatchString(id) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is a domain id, as GET /catalog/status lists them"})
		return
	}
	d, ok := rt.Scanner.DomainOf(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no domain " + id + " in the scanned table"})
		return
	}
	writeJSON(w, http.StatusOK, d)
}
