package cidomains

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/domainscore"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

// The two reads of the CI page (Work > CI) about domains (Owner 2026-11-16:
// he could not tell from the page who depends on whom, or what a change pulls
// in). Both are contracts, version Version in the body; read-only;
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
// changesRetry.

const (
	changesKeep   = 4096
	changesRetry  = 10 * time.Minute
	changesAtOnce = 4
)

// Source is what the two routes read.
type Source struct {
	Head func(ctx context.Context, repo string) (string, error)
	Tree func(ctx context.Context, repo, commit string) (*unitplan.Tree, error)
	// Files are the paths a commit changed against its first parent.
	Files func(ctx context.Context, repo, commit string) ([]string, error)
	// CommitOK says whether a kept commit id is one (nil: every id is kept).
	CommitOK func(string) bool
}

// Cache keeps the domain graphs and the files of commits.
type Cache struct {
	// graphMu guards graphs and reading. A graph is read in the background,
	// once a commit; a request never waits for one when an earlier graph of
	// the repository is known.
	graphMu sync.Mutex
	graphs  map[string]Graph      // by repo: the last graph read
	reading map[string]*graphRead // by repo: the read under way
	loaded  map[string]bool       // by repo: the kept file was looked at
	looked  map[string]time.Time  // by repo: when the kept file was last looked at for a newer graph

	// kept is where the last graph and the files of commits outlive the
	// process; nil is the platform's state store (StateKept).
	kept Kept

	mu      sync.Mutex
	places  map[string]placesKept // by repo
	changed map[string]changedKept
	// changedLoaded: by repo, the kept commits were read into changed.
	changedLoaded map[string]bool
}

// graphRead is one read of a graph under way.
type graphRead struct {
	commit string
	done   chan struct{}
	graph  Graph
	err    error
}

// Kept keeps small JSON by name across restarts. A missing name reads as
// nil, nil.
type Kept interface {
	Read(name string) ([]byte, error)
	Write(name string, raw []byte) error
}

// StateKept is the platform's state store (statefile: ConfigMaps in the
// cluster, files beside a local process), under ci/. With no data directory
// nothing is kept and every process reads anew.
type StateKept struct{}

func (StateKept) path(name string) string {
	if dir := strings.TrimSpace(os.Getenv("PLATFORM_DATA_DIR")); dir != "" {
		return filepath.Join(dir, "ci", name+".json")
	}
	return ""
}

func (k StateKept) Read(name string) ([]byte, error) {
	p := k.path(name)
	if p == "" {
		return nil, nil
	}
	raw, err := statefile.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return raw, err
}

func (k StateKept) Write(name string, raw []byte) error {
	p := k.path(name)
	if p == "" {
		return nil
	}
	return statefile.WriteFile(p, raw, 0o600)
}

func (c *Cache) store() Kept {
	if c.kept != nil {
		return c.kept
	}
	return StateKept{}
}

type placesKept struct {
	commit   string
	measured bool
	note     string
	places   Places
}

type changedKept struct {
	read  bool
	files []string
	at    time.Time
}

// goRoots are the directories of the repository's Go modules: the ones its
// declaration names, else the ones the ownership table's rows are under.
func goRoots(t *unitplan.Tree, table Table) []string {
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

// tableOf reads the ownership table of a tree: ok false with a note for a
// repository that has none.
func tableOf(t *unitplan.Tree) (table Table, note string, err error) {
	if _, ok := t.Lookup(OwnershipPath); !ok {
		return table, "this repository has no " + OwnershipPath + ": its domains are not measured", nil
	}
	raw, err := t.ReadFile(OwnershipPath)
	if err != nil {
		return table, "", err
	}
	if table, err = ParseTable(raw); err != nil {
		return table, OwnershipPath + ": " + err.Error(), nil
	}
	if _, ok := t.Lookup(ConsolePath); !ok {
		return table, "", nil
	}
	if raw, err = t.ReadFile(ConsolePath); err != nil {
		return table, "", err
	}
	if err = table.AddConsole(raw); err != nil {
		return table, ConsolePath + ": " + err.Error(), nil
	}
	return table, "", nil
}

func readAt(ctx context.Context, src Source, repo, commit string) (Graph, error) {
	t, err := src.Tree(ctx, repo, commit)
	if err != nil {
		return Graph{}, err
	}
	table, note, err := tableOf(t)
	if err != nil {
		return Graph{}, err
	}
	if note != "" {
		return Unmeasured(repo, commit, note), nil
	}
	var packages []domainscore.Package
	for _, root := range goRoots(t, table) {
		if err = ctx.Err(); err != nil {
			return Graph{}, err
		}
		mod, err := unitplan.ReadGoModule(t, root)
		if err != nil {
			return Graph{}, err
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
		return Unmeasured(repo, commit, "no Go package was found under the directories the ownership table names"), nil
	}
	return table.WithTiers(GraphOf(repo, commit, table.Ownership, packages)), nil
}

// Graph answers with the graph of main's head when it is known, and
// otherwise with the last graph that was read, marked Refreshing, while the
// head's is read in the background. Only a repository no graph was ever read
// of makes the request wait.
//
// Owner 2026-11-16 22:5x: switching to By dependency showed "loading" for a
// long time. The graph was read when the page asked, kept in memory only,
// and valid for one commit: main moved dozens of times that evening and the
// platform restarted at every delivery, so almost every look began a read of
// every Go file. Now the last graph is kept in the platform's state and
// shown at once, with the commit it is of.
// Graph is the domain graph (see above).
func (c *Cache) Graph(ctx context.Context, src Source, repo string) (Graph, error) {
	head, err := src.Head(ctx, repo)
	if err != nil {
		return Graph{}, err
	}
	c.graphMu.Lock()
	if !c.loaded[repo] {
		if c.loaded == nil {
			c.loaded = map[string]bool{}
		}
		c.loaded[repo] = true
		if raw, rerr := c.store().Read("domains-" + repo); rerr == nil && len(raw) > 0 {
			var g Graph
			if json.Unmarshal(raw, &g) == nil && g.Version == Version && g.Repo == repo && g.Commit != "" {
				if c.graphs == nil {
					c.graphs = map[string]Graph{}
				}
				if _, ok := c.graphs[repo]; !ok {
					c.graphs[repo] = g
				}
			}
		}
	}
	last, known := c.graphs[repo]
	if known && last.Commit != head && time.Since(c.looked[repo]) > 5*time.Second {
		// Another process may have read this head already (the workers read
		// ahead, ciDomainsAhead): look at what is kept before reading here.
		if c.looked == nil {
			c.looked = map[string]time.Time{}
		}
		c.looked[repo] = time.Now()
		if raw, rerr := c.store().Read("domains-" + repo); rerr == nil && len(raw) > 0 {
			var g Graph
			if json.Unmarshal(raw, &g) == nil && g.Version == Version && g.Repo == repo && g.Commit == head {
				c.graphs[repo], last = g, g
			}
		}
	}
	if known && last.Commit == head {
		c.graphMu.Unlock()
		last.Head, last.Refreshing = head, false
		return last, nil
	}
	read := c.reading[repo]
	if read == nil || read.commit != head {
		read = &graphRead{commit: head, done: make(chan struct{})}
		if c.reading == nil {
			c.reading = map[string]*graphRead{}
		}
		c.reading[repo] = read
		safego.Go("ci-domains-read", func() { c.readGraph(src, repo, read) })
	}
	c.graphMu.Unlock()
	if known {
		last.Head, last.Refreshing = head, true
		return last, nil
	}
	select {
	case <-read.done:
		if read.err != nil {
			return Graph{}, read.err
		}
		out := read.graph
		out.Head = head
		return out, nil
	case <-ctx.Done():
		return Graph{}, ctx.Err()
	}
}

// readGraph reads the graph of one commit away from any request, keeps it
// and writes it to the platform's state.
func (c *Cache) readGraph(src Source, repo string, read *graphRead) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	read.graph, read.err = readAt(ctx, src, repo, read.commit)
	c.graphMu.Lock()
	if read.err == nil {
		if c.graphs == nil || len(c.graphs) > 64 {
			c.graphs = map[string]Graph{}
		}
		c.graphs[repo] = read.graph
	}
	if c.reading[repo] == read {
		delete(c.reading, repo)
	}
	c.graphMu.Unlock()
	if read.err == nil {
		if raw, err := json.Marshal(read.graph); err == nil {
			if err = c.store().Write("domains-"+repo, raw); err != nil {
				slog.Warn("ci domains: the graph was not kept", "repo", repo, "error", err)
			}
		}
	} else {
		slog.Warn("ci domains: the graph was not read", "repo", repo, "commit", read.commit, "error", read.err)
	}
	close(read.done)
}

// placesOf is the ownership table and the package directories of main's head,
// kept while main stays there. It reads the tree's listing and one file.
func (c *Cache) placesOf(ctx context.Context, src Source, repo string) (placesKept, error) {
	head, err := src.Head(ctx, repo)
	if err != nil {
		return placesKept{}, err
	}
	c.mu.Lock()
	kept, ok := c.places[repo]
	c.mu.Unlock()
	if ok && kept.commit == head {
		return kept, nil
	}
	t, err := src.Tree(ctx, repo, head)
	if err != nil {
		return placesKept{}, err
	}
	table, note, err := tableOf(t)
	if err != nil {
		return placesKept{}, err
	}
	kept = placesKept{commit: head, measured: note == "", note: note, places: Places{Table: table, IsPackage: func(dir string) bool {
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
		c.places = map[string]placesKept{}
	}
	c.places[repo] = kept
	return kept, nil
}

// Changes counts, by domain, the files each commit changed.
func (c *Cache) Changes(ctx context.Context, src Source, now time.Time, repo string, commits []string) (Changes, error) {
	places, err := c.placesOf(ctx, src, repo)
	if err != nil {
		return Changes{}, err
	}
	if !places.measured {
		return Changes{Version: Version, Repo: repo, Note: places.note, Changes: []Change{}}, nil
	}
	c.loadChanged(src, repo)
	// What is not kept is read, a few commits at a time.
	results := make([]changedKept, len(commits))
	var wg sync.WaitGroup
	var readNow atomic.Bool
	gate := make(chan struct{}, changesAtOnce)
	for i, commit := range commits {
		key := repo + "\x00" + commit
		c.mu.Lock()
		kept, ok := c.changed[key]
		c.mu.Unlock()
		if ok && (kept.read || now.Sub(kept.at) < changesRetry) {
			results[i] = kept
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer safego.Recover("ci-changes")
			gate <- struct{}{}
			defer func() { <-gate }()
			files, err := src.Files(ctx, repo, commit)
			got := changedKept{read: err == nil, files: files, at: now}
			results[i] = got
			if got.read {
				readNow.Store(true)
			}
			if ctx.Err() != nil {
				return // a read cut short is not "the repository does not have it"
			}
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.changed == nil || len(c.changed) >= changesKeep {
				c.changed = map[string]changedKept{} // start over rather than track ages
			}
			c.changed[key] = got
		}()
	}
	wg.Wait()
	if readNow.Load() {
		c.saveChanged(repo, commits, results)
	}
	out := Changes{Version: Version, Repo: repo, Measured: true, Changes: make([]Change, 0, len(commits))}
	for i, commit := range commits {
		change := Change{Commit: commit, Read: results[i].read, Domains: map[string]int{}}
		if change.Read {
			change.Domains = places.places.Count(results[i].files)
		}
		out.Changes = append(out.Changes, change)
	}
	return out, nil
}

// The files of commits outlive the process too: a commit never changes, and
// after a restart the page asked Gitea again for every run it shows (some
// hundred commits). changesKeptCommits newest-asked commits are kept, each
// with at most changesKeptFiles paths (a larger commit is read again), so
// the kept state stays far under a ConfigMap's megabyte.
const (
	changesKeptCommits = 300
	changesKeptFiles   = 150
)

func (c *Cache) loadChanged(src Source, repo string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.changedLoaded[repo] {
		return
	}
	if c.changedLoaded == nil {
		c.changedLoaded = map[string]bool{}
	}
	c.changedLoaded[repo] = true
	raw, err := c.store().Read("changes-" + repo)
	if err != nil || len(raw) == 0 {
		return
	}
	var kept map[string][]string
	if json.Unmarshal(raw, &kept) != nil {
		return
	}
	if c.changed == nil {
		c.changed = map[string]changedKept{}
	}
	for commit, files := range kept {
		if src.CommitOK == nil || src.CommitOK(commit) {
			c.changed[repo+"\x00"+commit] = changedKept{read: true, files: files}
		}
	}
}

// saveChanged writes the commits just asked for, when one of them was read
// from Gitea now: the ones asked for last are the ones a page shows.
func (c *Cache) saveChanged(repo string, commits []string, results []changedKept) {
	kept := map[string][]string{}
	for i, commit := range commits {
		r := results[i]
		if !r.read || len(r.files) > changesKeptFiles || len(kept) >= changesKeptCommits {
			continue
		}
		kept[commit] = r.files
	}
	if len(kept) == 0 {
		return
	}
	raw, err := json.Marshal(kept)
	if err == nil {
		err = c.store().Write("changes-"+repo, raw)
	}
	if err != nil {
		slog.Warn("ci changes: the files of commits were not kept", "repo", repo, "error", err)
	}
}

// ChangedFilesOf reads Gitea's listing of one commit (git's --first-parent
// name status, renames as a removal and an addition) into the paths it
// changed.
func ChangedFilesOf(body []byte, repo, commit string) ([]string, error) {
	var doc struct {
		SHA   string `json:"sha"`
		Files *[]struct {
			Filename string `json:"filename"`
		} `json:"files"`
	}
	if err := json.Unmarshal(body, &doc); err != nil || doc.SHA != commit {
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

// Route is the two routes about domains. RepoOK says whether a name is a
// repository's; Source says, per request, what to read (ok false: this
// platform does not read Gitea).
type Route struct {
	Cache  *Cache
	RepoOK func(string) bool
	Source func() (Source, bool)
}

func routeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (rt Route) repoParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	repo := strings.TrimSpace(r.URL.Query().Get("repo"))
	if !rt.RepoOK(repo) || strings.HasSuffix(repo, ".git") {
		routeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo is a repository name"})
		return "", false
	}
	return repo, true
}

// HandleDomains is GET /ci/domains.
func (rt Route) HandleDomains(w http.ResponseWriter, r *http.Request) {
	repo, ok := rt.repoParam(w, r)
	if !ok {
		return
	}
	src, ok := rt.Source()
	if !ok {
		routeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "this platform does not read Gitea"})
		return
	}
	// As /units/config: a first read of a module is every Go file of it.
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	out, err := rt.Cache.Graph(ctx, src, repo)
	if err != nil {
		routeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	routeJSON(w, http.StatusOK, out)
}

// HandleChanges is GET /ci/changes.
func (rt Route) HandleChanges(w http.ResponseWriter, r *http.Request) {
	repo, ok := rt.repoParam(w, r)
	if !ok {
		return
	}
	commits, err := ParseCommits(r.URL.Query().Get("commits"))
	if err != nil {
		routeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	src, ok := rt.Source()
	if !ok {
		routeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "this platform does not read Gitea"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	out, err := rt.Cache.Changes(ctx, src, time.Now(), repo, commits)
	if err != nil {
		routeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	routeJSON(w, http.StatusOK, out)
}
