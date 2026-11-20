package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
)

// Every is how often the scanner reads each repository's main again.
const Every = 10 * time.Minute

// Tree is one commit of a repository as the scan reads it.
type Tree struct {
	Commit string
	Files  []string
	// Read returns a file's bytes; ok false for a path the tree does not have.
	Read func(path string) (raw []byte, ok bool, err error)
}

// Source reads the tree of a repository's main head. The platform hands it
// Gitea's tree reader; this package does not know where trees come from.
type Source func(ctx context.Context, repo string) (Tree, error)

// Kept keeps small JSON by name across restarts. A missing name reads as
// nil, nil.
type Kept interface {
	Read(name string) ([]byte, error)
	Write(name string, raw []byte) error
}

// StateKept is the platform's state store under catalog/ (ConfigMaps in the
// cluster, files beside a local process); with no data directory nothing is
// kept.
type StateKept struct{}

func (StateKept) path(name string) string {
	if dir := strings.TrimSpace(os.Getenv("PLATFORM_DATA_DIR")); dir != "" {
		return filepath.Join(dir, "catalog", name+".json")
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

var repoName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

func keptName(repo string) string { return "unperceived-" + repo }

func keptOwners(repo string) string { return "owners-" + repo }

// keptIndex is the index as the last scan of a repository that has one read it.
const keptIndex = "index"

// PoolOf scans one tree: a repository with no ownership table is one entry,
// the whole repository.
func PoolOf(repo string, t Tree) Pool {
	p, _ := poolAndTable(repo, t)
	return p
}

// poolAndTable is PoolOf and the table it read (nil: none read).
func poolAndTable(repo string, t Tree) (Pool, *Table) {
	p := Pool{Version: Version, Repo: repo, Commit: t.Commit, Measured: true, Entries: []Entry{}}
	own, ok, err := t.Read(OwnershipPath)
	if err != nil {
		p.Measured, p.Note = false, err.Error()
		return p, nil
	}
	if !ok {
		p.Note = "this repository has no " + OwnershipPath + ": the map perceives none of it"
		p.Entries = []Entry{{Path: ".", Kind: "repository", Files: len(t.Files)}}
		return p, nil
	}
	console, _, err := t.Read(ConsolePath)
	if err != nil {
		p.Measured, p.Note = false, err.Error()
		return p, nil
	}
	table, err := ParseTable(own, console)
	if err != nil {
		p.Measured, p.Note = false, err.Error()
		return p, nil
	}
	goDirs := map[string]bool{}
	for _, f := range t.Files {
		if strings.HasSuffix(f, ".go") {
			goDirs[path.Dir(f)] = true
		}
	}
	p.Entries = Scan(table, t.Files, func(dir string) bool { return goDirs[dir] })
	return p, &table
}

// Scanner scans repositories and keeps each one's last pool.
type Scanner struct {
	Source Source
	Kept   Kept // nil: StateKept
	Now    func() time.Time

	mu sync.Mutex // one scan at a time
}

func (s *Scanner) kept() Kept {
	if s.Kept != nil {
		return s.Kept
	}
	return StateKept{}
}

func (s *Scanner) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

// Last is the pool last kept for repo, nil when none.
func (s *Scanner) Last(repo string) (*Pool, error) {
	raw, err := s.kept().Read(keptName(repo))
	if err != nil || raw == nil {
		return nil, err
	}
	var p Pool
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// ScanOnce scans repo's main and keeps the pool. An unreadable tree keeps an
// unmeasured pool that carries the last entries over, so first-seen times
// are not lost to one failed read.
func (s *Scanner) ScanOnce(ctx context.Context, repo string) (Pool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	before, _ := s.Last(repo)
	now := s.now()
	t, err := s.Source(ctx, repo)
	var p Pool
	if err != nil {
		p = Pool{Version: Version, Repo: repo, Note: "main could not be read: " + err.Error(), Entries: []Entry{}}
		if before != nil {
			p.Commit, p.Entries = before.Commit, before.Entries
		}
	} else {
		var table *Table
		p, table = poolAndTable(repo, t)
		if !p.Measured && before != nil {
			p.Entries = before.Entries
		}
		if p.Measured {
			s.keepTables(repo, t, table, now)
		}
	}
	p.ScannedAt = now
	reviews := map[string]Review{}
	if err == nil && p.Measured {
		if raw, ok, rerr := t.Read(ReviewPath); rerr == nil && ok {
			if r, perr := ParseReviews(raw); perr == nil {
				reviews = r
			} else {
				p.Note = strings.TrimSpace(p.Note + " " + perr.Error())
			}
		}
	} else if before != nil {
		for _, e := range before.Entries {
			if e.Review != nil {
				reviews[e.Path] = *e.Review
			}
		}
	}
	p.Summarize(now, before, reviews)
	if !p.Measured && before == nil {
		// Nothing was read and nothing was kept: keeping an empty unmeasured
		// pool would only say what the route says without it.
		return p, nil
	}
	raw, merr := json.Marshal(p)
	if merr != nil {
		return p, merr
	}
	return p, s.kept().Write(keptName(repo), raw)
}

// keepTables keeps what "who owns X" and the index are answered from. A
// failure to keep them is logged; the pool is kept either way.
func (s *Scanner) keepTables(repo string, t Tree, table *Table, now time.Time) {
	o := Owners{Repo: repo, Commit: t.Commit, ScannedAt: now, Packages: map[string][]string{}, Groups: map[string][]string{}}
	if table != nil {
		o = ownersOf(repo, t.Commit, *table, now)
	}
	if raw, err := json.Marshal(o); err == nil {
		if err := s.kept().Write(keptOwners(repo), raw); err != nil {
			slog.Warn("catalog: keep owners", "repo", repo, "err", err)
		}
	}
	if table == nil {
		return
	}
	raw, ok, err := t.Read(IndexPath)
	if err != nil || !ok {
		return
	}
	files := map[string]bool{}
	for _, f := range t.Files {
		files[f] = true
		for d := path.Dir(f); d != "." && !files[d]; d = path.Dir(d) {
			files[d] = true
		}
	}
	ix, err := ParseIndex(raw, t.Commit, func(p string) bool {
		if strings.Contains(p, ":") || strings.Contains(p, " ") {
			return true // another repository or a place outside any: not checked here
		}
		return files[p]
	}, *table)
	if err != nil {
		slog.Warn("catalog: index", "repo", repo, "err", err)
		return
	}
	if out, err := json.Marshal(ix); err == nil {
		if err := s.kept().Write(keptIndex, out); err != nil {
			slog.Warn("catalog: keep index", "err", err)
		}
	}
}

func (s *Scanner) lastOwners(repo string) (*Owners, error) {
	raw, err := s.kept().Read(keptOwners(repo))
	if err != nil || raw == nil {
		return nil, err
	}
	var o Owners
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, err
	}
	return &o, nil
}

// Run scans every repository now and then every Every until ctx ends.
func (s *Scanner) Run(ctx context.Context, repos []string) {
	safego.Go("catalog-scan", func() {
		tick := time.NewTicker(Every)
		defer tick.Stop()
		for {
			for _, repo := range repos {
				c, cancel := context.WithTimeout(ctx, 2*time.Minute)
				if _, err := s.ScanOnce(c, repo); err != nil {
					slog.Warn("catalog: scan", "repo", repo, "err", err)
				}
				cancel()
			}
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	})
}

// Route is GET /catalog/unperceived?repo=…: the pool last kept. It reads the
// kept pool only; it never reads Gitea or the cluster on a request.
type Route struct {
	Scanner *Scanner
}

func (rt Route) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	repo := strings.TrimSpace(r.URL.Query().Get("repo"))
	if !repoName.MatchString(repo) || strings.HasSuffix(repo, ".git") {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "repo is a repository name"})
		return
	}
	p, err := rt.Scanner.Last(repo)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if p == nil {
		p = &Pool{Version: Version, Repo: repo, Note: "not scanned yet: the scanner runs in the workers every " + Every.String(), Entries: []Entry{}}
	}
	_ = json.NewEncoder(w).Encode(p)
}
