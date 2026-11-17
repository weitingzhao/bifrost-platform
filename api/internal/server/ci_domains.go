package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/cidomains"
	"github.com/weitingzhao/bifrost-platform/api/internal/domainscore"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

// The two reads of the CI page (Work > CI) about domains (Owner 2026-11-16:
// he could not tell from the page who depends on whom, or what a change pulls
// in). Both are contracts, version cidomains.Version in the body; read-only;
// viewer and above.
//
//	GET /api/v1/ci/domains?repo=…            the domain graph of main's head
//	GET /api/v1/ci/changes?repo=…&commits=…  what each commit changed, by domain
//
// This process has no checkout and runs no go command: the repository is read
// at main's head through the Gitea tree reader the unit plan uses, and the
// imports are the ones go/build reads from the files (unitplan.ReadGoModule).
// Nothing reads the cluster.
//
// The graph is kept for as long as main stays at the commit it was read at:
// a first read fetches every Go file of the module (the blobs are kept by the
// tree reader, so the next commit reads only what it changed). A commit's
// changed files never change and are kept until the process ends or the
// store is full; a commit that could not be read is asked again after
// ciChangesRetry.

const (
	ciChangesKeep   = 4096
	ciChangesRetry  = 10 * time.Minute
	ciChangesAtOnce = 4
)

// ciDomainsSource is what the two routes read; the server's is ciDomainsSourceOf.
type ciDomainsSource struct {
	head func(ctx context.Context, repo string) (string, error)
	tree func(ctx context.Context, repo, commit string) (*unitplan.Tree, error)
	// files are the paths a commit changed against its first parent.
	files func(ctx context.Context, repo, commit string) ([]string, error)
}

type ciDomainsCache struct {
	// graphMu is held across a read of the graph, so requests that arrive
	// together read once.
	graphMu sync.Mutex
	graphs  map[string]cidomains.Graph // by repo; valid while Commit is main's head

	mu      sync.Mutex
	places  map[string]ciPlacesKept // by repo
	changed map[string]ciChangedKept
}

type ciPlacesKept struct {
	commit   string
	measured bool
	note     string
	places   cidomains.Places
}

type ciChangedKept struct {
	read  bool
	files []string
	at    time.Time
}

// ciGoRoots are the directories of the repository's Go modules: the ones its
// declaration names, else the ones the ownership table's rows are under.
func ciGoRoots(t *unitplan.Tree, table cidomains.Table) []string {
	var roots []string
	if doc, err := t.ReadFile(unitplan.ManifestPath); err == nil {
		if manifest, err := unitplan.ParseManifest(doc); err == nil {
			for _, m := range manifest.Modules {
				if m.Kind == "go" {
					roots = append(roots, strings.Trim(m.Root, "/"))
				}
			}
		}
	}
	if len(roots) > 0 {
		return roots
	}
	for _, root := range table.GoRoots() {
		if _, ok := t.Lookup(root + "/go.mod"); ok {
			roots = append(roots, root)
		}
	}
	return roots
}

// ciTable reads the ownership table of a tree: ok false with a note for a
// repository that has none.
func ciTable(t *unitplan.Tree) (table cidomains.Table, note string, err error) {
	if _, ok := t.Lookup(cidomains.OwnershipPath); !ok {
		return table, "this repository has no " + cidomains.OwnershipPath + ": its domains are not measured", nil
	}
	raw, err := t.ReadFile(cidomains.OwnershipPath)
	if err != nil {
		return table, "", err
	}
	if table, err = cidomains.ParseTable(raw); err != nil {
		return table, cidomains.OwnershipPath + ": " + err.Error(), nil
	}
	return table, "", nil
}

func readCIDomains(ctx context.Context, src ciDomainsSource, repo, commit string) (cidomains.Graph, error) {
	t, err := src.tree(ctx, repo, commit)
	if err != nil {
		return cidomains.Graph{}, err
	}
	table, note, err := ciTable(t)
	if err != nil {
		return cidomains.Graph{}, err
	}
	if note != "" {
		return cidomains.Unmeasured(repo, commit, note), nil
	}
	var packages []domainscore.Package
	for _, root := range ciGoRoots(t, table) {
		if err = ctx.Err(); err != nil {
			return cidomains.Graph{}, err
		}
		mod, err := unitplan.ReadGoModule(t, root)
		if err != nil {
			return cidomains.Graph{}, err
		}
		for _, p := range mod.Packages {
			pkg := domainscore.Package{Path: p.Dir}
			for _, ip := range p.Imports {
				if dep := mod.Packages[ip]; dep != nil {
					pkg.Imports = append(pkg.Imports, dep.Dir)
				}
			}
			packages = append(packages, pkg)
		}
	}
	if len(packages) == 0 {
		return cidomains.Unmeasured(repo, commit, "no Go package was found under the directories the ownership table names"), nil
	}
	return cidomains.GraphOf(repo, commit, table.Ownership, packages), nil
}

func (c *ciDomainsCache) graph(ctx context.Context, src ciDomainsSource, repo string) (cidomains.Graph, error) {
	head, err := src.head(ctx, repo)
	if err != nil {
		return cidomains.Graph{}, err
	}
	c.graphMu.Lock()
	defer c.graphMu.Unlock()
	if kept, ok := c.graphs[repo]; ok && kept.Commit == head {
		return kept, nil
	}
	out, err := readCIDomains(ctx, src, repo, head)
	if err != nil {
		return cidomains.Graph{}, err
	}
	if c.graphs == nil || len(c.graphs) > 64 {
		c.graphs = map[string]cidomains.Graph{}
	}
	c.graphs[repo] = out
	return out, nil
}

// placesOf is the ownership table and the package directories of main's head,
// kept while main stays there. It reads the tree's listing and one file.
func (c *ciDomainsCache) placesOf(ctx context.Context, src ciDomainsSource, repo string) (ciPlacesKept, error) {
	head, err := src.head(ctx, repo)
	if err != nil {
		return ciPlacesKept{}, err
	}
	c.mu.Lock()
	kept, ok := c.places[repo]
	c.mu.Unlock()
	if ok && kept.commit == head {
		return kept, nil
	}
	t, err := src.tree(ctx, repo, head)
	if err != nil {
		return ciPlacesKept{}, err
	}
	table, note, err := ciTable(t)
	if err != nil {
		return ciPlacesKept{}, err
	}
	kept = ciPlacesKept{commit: head, measured: note == "", note: note, places: cidomains.Places{Table: table, IsPackage: func(dir string) bool {
		for _, name := range t.Names(dir) {
			if strings.HasSuffix(name, ".go") {
				if e, _ := t.Lookup(dir + "/" + name); !e.Dir {
					return true
				}
			}
		}
		return false
	}}}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.places == nil || len(c.places) > 64 {
		c.places = map[string]ciPlacesKept{}
	}
	c.places[repo] = kept
	return kept, nil
}

func (c *ciDomainsCache) changes(ctx context.Context, src ciDomainsSource, now time.Time, repo string, commits []string) (cidomains.Changes, error) {
	places, err := c.placesOf(ctx, src, repo)
	if err != nil {
		return cidomains.Changes{}, err
	}
	if !places.measured {
		return cidomains.Changes{Version: cidomains.Version, Repo: repo, Note: places.note, Changes: []cidomains.Change{}}, nil
	}
	// What is not kept is read, a few commits at a time.
	results := make([]ciChangedKept, len(commits))
	var wg sync.WaitGroup
	gate := make(chan struct{}, ciChangesAtOnce)
	for i, commit := range commits {
		key := repo + "\x00" + commit
		c.mu.Lock()
		kept, ok := c.changed[key]
		c.mu.Unlock()
		if ok && (kept.read || now.Sub(kept.at) < ciChangesRetry) {
			results[i] = kept
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer safego.Recover("ci-changes")
			gate <- struct{}{}
			defer func() { <-gate }()
			files, err := src.files(ctx, repo, commit)
			got := ciChangedKept{read: err == nil, files: files, at: now}
			results[i] = got
			if ctx.Err() != nil {
				return // a read cut short is not "the repository does not have it"
			}
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.changed == nil || len(c.changed) >= ciChangesKeep {
				c.changed = map[string]ciChangedKept{} // start over rather than track ages
			}
			c.changed[key] = got
		}()
	}
	wg.Wait()
	out := cidomains.Changes{Version: cidomains.Version, Repo: repo, Measured: true, Changes: make([]cidomains.Change, 0, len(commits))}
	for i, commit := range commits {
		change := cidomains.Change{Commit: commit, Read: results[i].read, Domains: map[string]int{}}
		if change.Read {
			change.Domains = places.places.Count(results[i].files)
		}
		out.Changes = append(out.Changes, change)
	}
	return out, nil
}

func (s *Server) ciDomainsSourceOf() ciDomainsSource {
	return ciDomainsSource{head: s.mainHead, tree: s.unitTreesOf(), files: s.giteaChangedFiles}
}

// giteaChangedFiles are the paths a commit changed against its first parent,
// as Gitea lists them for the commit (git's --first-parent name status,
// renames as a removal and an addition).
func (s *Server) giteaChangedFiles(ctx context.Context, repo, commit string) ([]string, error) {
	s.unitTreesOf()
	body, err := s.unitTrees.get(ctx, url.PathEscape(repo)+"/git/commits/"+commit+"?stat=false&verification=false&files=true", 8<<20)
	if err != nil {
		return nil, err
	}
	var doc struct {
		SHA   string `json:"sha"`
		Files *[]struct {
			Filename string `json:"filename"`
		} `json:"files"`
	}
	if err = json.Unmarshal(body, &doc); err != nil || doc.SHA != commit {
		return nil, fmt.Errorf("gitea: %s commit %s: not that commit (%v)", repo, commit, err)
	}
	if doc.Files == nil {
		return nil, fmt.Errorf("gitea: %s commit %s: listed without its files", repo, commit)
	}
	out := make([]string, 0, len(*doc.Files))
	for _, f := range *doc.Files {
		out = append(out, f.Filename)
	}
	return out, nil
}

func ciRepoParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	repo := strings.TrimSpace(r.URL.Query().Get("repo"))
	if !giteaName.MatchString(repo) || strings.HasSuffix(repo, ".git") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo is a repository name"})
		return "", false
	}
	return repo, true
}

func (s *Server) handleCIDomains(w http.ResponseWriter, r *http.Request) {
	repo, ok := ciRepoParam(w, r)
	if !ok {
		return
	}
	if s.delivery == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "this platform does not read Gitea"})
		return
	}
	// As /units/config: a first read of a module is every Go file of it.
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	out, err := s.ciDomainsKept.graph(ctx, s.ciDomainsSourceOf(), repo)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCIChanges(w http.ResponseWriter, r *http.Request) {
	repo, ok := ciRepoParam(w, r)
	if !ok {
		return
	}
	commits, err := cidomains.ParseCommits(r.URL.Query().Get("commits"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if s.delivery == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "this platform does not read Gitea"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	out, err := s.ciDomainsKept.changes(ctx, s.ciDomainsSourceOf(), time.Now(), repo, commits)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}
