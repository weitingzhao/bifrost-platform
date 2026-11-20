package catalog

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"
)

// IndexPath is the map's table of contents: every class of thing and its one
// source (read from the platform's own repository).
const IndexPath = "config/catalog-index.json"

// Owners is what the scan keeps of one repository's tables so that "who owns
// X" is answered without reading Gitea: each row's and group's domains.
type Owners struct {
	Repo      string              `json:"repo"`
	Commit    string              `json:"commit"`
	ScannedAt time.Time           `json:"scanned_at"`
	HasTable  bool                `json:"has_table"`
	GoRoots   []string            `json:"go_roots"`
	Console   string              `json:"console"`
	Packages  map[string][]string `json:"packages"`
	Groups    map[string][]string `json:"groups"`
}

func ownersOf(repo, commit string, t Table, now time.Time) Owners {
	o := Owners{Repo: repo, Commit: commit, ScannedAt: now, HasTable: true, GoRoots: t.GoRoots, Console: t.Console,
		Packages: map[string][]string{}, Groups: map[string][]string{}}
	for p := range t.Packages {
		o.Packages[p] = t.Domains[p]
	}
	for _, g := range t.Groups {
		o.Groups[g] = t.Domains[g]
	}
	return o
}

// Owner is the answer of GET /api/v1/catalog/owner.
type Owner struct {
	Version int    `json:"version"`
	Repo    string `json:"repo"`
	Path    string `json:"path"`
	Commit  string `json:"commit"`
	// Perceived is true when a row or a group covers the path. Domains are
	// the domains it names (more than one: a shared row, counted debt); By
	// says which row or group ("package api/internal/x", "group console/src/y").
	Perceived bool     `json:"perceived"`
	Domains   []string `json:"domains"`
	By        string   `json:"by"`
	// Entry is the pool entry the path falls in when nothing covers it.
	Entry *Entry `json:"entry,omitempty"`
	Note  string `json:"note"`
}

// OwnerOf answers who owns p from what the scan kept (pool may be nil).
func OwnerOf(o *Owners, pool *Pool, p string) Owner {
	p = strings.Trim(path.Clean("/"+p), "/")
	out := Owner{Version: Version, Path: p, Domains: []string{}}
	if o == nil {
		out.Note = "not scanned yet"
		return out
	}
	out.Repo, out.Commit = o.Repo, o.Commit
	if o.HasTable {
		top, _, _ := strings.Cut(p, "/")
		if o.Console != "" && top == o.Console {
			best := ""
			for g := range o.Groups {
				if (p == g || strings.HasPrefix(p, g+"/")) && len(g) > len(best) {
					best = g
				}
			}
			if best != "" {
				return covered(out, o.Groups[best], "group "+best)
			}
		}
		for _, r := range o.GoRoots {
			if r != top {
				continue
			}
			for dir := p; strings.Contains(dir, "/"); dir = path.Dir(dir) {
				if d, ok := o.Packages[dir]; ok {
					return covered(out, d, "package "+dir)
				}
			}
		}
	}
	if pool != nil {
		for i := range pool.Entries {
			e := pool.Entries[i]
			atRoot := e.Kind == "root" && !strings.Contains(p, "/")
			if atRoot || e.Kind == "repository" || p == e.Path || strings.HasPrefix(p, e.Path+"/") {
				out.Entry = &e
				break
			}
		}
	}
	out.Note = "cannot be perceived: no row or group covers it"
	return out
}

func covered(out Owner, domains []string, by string) Owner {
	out.Perceived, out.By = true, by
	if domains != nil {
		out.Domains = domains
	}
	if len(domains) > 1 {
		out.Note = fmt.Sprintf("shared by %d domains: counted debt (only one owner is the rule)", len(domains))
	}
	return out
}

// Class is one line of the index.
type Class struct {
	Class   string   `json:"class"`
	What    string   `json:"what"`
	Sources []string `json:"sources"`
	Reader  string   `json:"reader,omitempty"`
	Also    []string `json:"also,omitempty"`
	Note    string   `json:"note,omitempty"`
}

// Index is IndexPath, and what the scan found about it.
type Index struct {
	Version int     `json:"version"`
	Commit  string  `json:"commit"`
	Classes []Class `json:"classes"`
	// Domain is each class's domain: its reader's row in the ownership table
	// ("" with no reader or no row).
	Domain map[string][]string `json:"domain"`
	// Missing are sources the index names that the commit does not have.
	Missing []string `json:"missing"`
	// Measured counts classes with a source; Unmeasured those with none;
	// Duplicated those with a second place that says the same class.
	Measured   int `json:"measured"`
	Unmeasured int `json:"unmeasured"`
	Duplicated int `json:"duplicated"`
}

// ParseIndex reads IndexPath and checks it against the tree: every class
// once, every in-repository source present.
func ParseIndex(raw []byte, commit string, has func(string) bool, t Table) (Index, error) {
	var ix Index
	if err := json.Unmarshal(raw, &ix); err != nil {
		return ix, fmt.Errorf("%s does not parse: %w", IndexPath, err)
	}
	ix.Commit, ix.Domain, ix.Missing = commit, map[string][]string{}, []string{}
	seen := map[string]bool{}
	for _, c := range ix.Classes {
		if c.Class == "" || seen[c.Class] {
			return ix, fmt.Errorf("%s: class %q is empty or named twice", IndexPath, c.Class)
		}
		seen[c.Class] = true
		for _, s := range c.Sources {
			if has != nil && !has(strings.TrimSuffix(s, "/")) {
				ix.Missing = append(ix.Missing, s)
			}
		}
		if c.Reader != "" {
			ix.Domain[c.Class] = t.Domains[c.Reader]
		}
		if len(c.Sources) == 0 {
			ix.Unmeasured++
		} else {
			ix.Measured++
		}
		if len(c.Also) > 0 {
			ix.Duplicated++
		}
	}
	return ix, nil
}

// OwnerRoute is GET /catalog/owner?repo=…&path=…, from what the scan kept.
type OwnerRoute struct{ Scanner *Scanner }

func (rt OwnerRoute) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	repo := strings.TrimSpace(r.URL.Query().Get("repo"))
	p := r.URL.Query().Get("path")
	if !repoName.MatchString(repo) || strings.HasSuffix(repo, ".git") || strings.TrimSpace(p) == "" || len(p) > 1024 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo is a repository name and path a path in it"})
		return
	}
	o, err := rt.Scanner.lastOwners(repo)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	pool, _ := rt.Scanner.Last(repo)
	writeJSON(w, http.StatusOK, OwnerOf(o, pool, p))
}

// IndexRoute is GET /catalog/index: the index as the last scan of the
// platform's repository read it.
type IndexRoute struct{ Scanner *Scanner }

func (rt IndexRoute) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, err := rt.Scanner.kept().Read(keptIndex)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if raw == nil {
		writeJSON(w, http.StatusOK, map[string]any{"version": Version, "note": "not scanned yet", "classes": []Class{}})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
