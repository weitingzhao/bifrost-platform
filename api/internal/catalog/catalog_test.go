package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"
)

// Origin of these cases: 0.2 map line, 2026-11-20. Owner (via coordinator):
// what is not on the map is "cannot be perceived", a defect to review; the
// scan never adds it to the map, and each entry carries when it was found.
// The 11-19 survey found the table's default (no row -> foundation) means a
// package with no row is never reported anywhere; these cases hold that the
// pool reports it while the default itself is left alone.

const ownJSON = `{"default":{"domains":["foundation"]},"packages":{
 "api/internal/a":{"domains":["plan"]},
 "api/internal/m":{"domains":["plan","lineage"]}}}`

const consoleJSON = `{"frontend":{"root":"console/src","groups":{"console/src":{},"console/src/pages/x":{}}}}`

func tree(files ...string) Tree {
	blobs := map[string]string{OwnershipPath: ownJSON, ConsolePath: consoleJSON}
	return Tree{Commit: "c1", Files: append(files, OwnershipPath, ConsolePath), Read: func(p string) ([]byte, bool, error) {
		b, ok := blobs[p]
		return []byte(b), ok, nil
	}}
}

func paths(es []Entry) map[string]string {
	out := map[string]string{}
	for _, e := range es {
		out[e.Path] = e.Kind
	}
	return out
}

func TestPoolListsWhatNoRowCovers(t *testing.T) {
	p := PoolOf("r", tree(
		"api/internal/a/a.go", "api/internal/a/testdata/x.json", // row
		"api/internal/m/m.go",       // mixed is still a row: on the map
		"api/internal/b/b.go",       // no row: in the pool although the default covers it
		"api/go.mod",                // in no package
		"console/src/pages/x/y.tsx", // a group
		"console/scripts/z.mjs",     // console, no group
		"mcp/platform/src/t.ts", "mcp/platform/package.json",
		"Makefile",
	))
	got := paths(p.Entries)
	want := map[string]string{
		"api/internal/b":  "go-package",
		"api":             "api",
		"console/scripts": "console",
		"console":         "console", // console/domain-ownership.json itself is in no group
		"mcp/platform":    "mcp",
		"config":          "config",
		".":               "root",
	}
	if len(got) != len(want) {
		t.Fatalf("pool = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("entry %s = %q, want %q (pool %v)", k, got[k], v, got)
		}
	}
}

// Positive control for the case above: a tree whose every file a row or a
// group covers has an empty pool (an empty pool must be reachable).
func TestPoolIsEmptyWhenEverythingIsOnTheMap(t *testing.T) {
	tr := tree("api/internal/a/a.go", "console/src/main.tsx")
	tr.Files = []string{"api/internal/a/a.go", "console/src/main.tsx"}
	if p := PoolOf("r", tr); len(p.Entries) != 0 || !p.Measured {
		t.Fatalf("pool = %+v, want measured and empty", p)
	}
}

func TestARepositoryWithNoTableIsOneEntry(t *testing.T) {
	p := PoolOf("other", Tree{Commit: "c", Files: []string{"a", "b/c"}, Read: func(string) ([]byte, bool, error) { return nil, false, nil }})
	if !p.Measured || len(p.Entries) != 1 || p.Entries[0].Kind != "repository" || p.Entries[0].Files != 2 {
		t.Fatalf("pool = %+v", p)
	}
}

type memKept map[string][]byte

func (m memKept) Read(n string) ([]byte, error)  { return m[n], nil }
func (m memKept) Write(n string, b []byte) error { m[n] = b; return nil }

func TestFirstSeenSurvivesScansAndAFailedRead(t *testing.T) {
	day := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	now := day
	fail := false
	s := &Scanner{Kept: memKept{}, Now: func() time.Time { return now }, Source: func(context.Context, string) (Tree, error) {
		if fail {
			return Tree{}, errors.New("gitea down")
		}
		return tree("mcp/x/a.ts"), nil
	}}
	if _, err := s.ScanOnce(context.Background(), "r"); err != nil {
		t.Fatal(err)
	}
	now, fail = day.Add(72*time.Hour), true
	p, _ := s.ScanOnce(context.Background(), "r")
	if p.Measured || p.Count == 0 {
		t.Fatalf("failed read: %+v, want unmeasured with the last entries", p)
	}
	fail = false
	p, _ = s.ScanOnce(context.Background(), "r")
	for _, e := range p.Entries {
		if !e.FirstSeen.Equal(day) {
			t.Errorf("%s first seen %v, want %v", e.Path, e.FirstSeen, day)
		}
	}
	if p.OldestDays == nil || *p.OldestDays != 3 || p.Unreviewed != p.Count {
		t.Fatalf("summary = %+v", p)
	}
}

func TestReviewsAttachAndOnlyDeleteOrUnknownStay(t *testing.T) {
	if _, err := ParseReviews([]byte(`{"reviews":{"mcp/x":{"verdict":"claim","reason":"r"}}}`)); err == nil {
		t.Fatal("a claim with no domain must be refused: a claim names who should own it")
	}
	if _, err := ParseReviews([]byte(`{"reviews":{"mcp/x":{"verdict":"delete"}}}`)); err == nil {
		t.Fatal("a verdict with no reason must be refused: every entry is looked at, and the look is written down")
	}
	if _, err := ParseReviews([]byte(`{"reviews":{"mcp/x":{"verdict":"claim","domain":"tenant","reason":"r"}}}`)); err != nil {
		t.Fatal(err)
	}
	tr := tree("mcp/x/a.ts")
	tr.Files = append(tr.Files, ReviewPath)
	read := tr.Read
	tr.Read = func(p string) ([]byte, bool, error) {
		if p == ReviewPath {
			return []byte(`{"reviews":{"mcp/x":{"verdict":"unknown","reason":"r","by":"seat","on":"2026-11-20"}}}`), true, nil
		}
		return read(p)
	}
	s := &Scanner{Kept: memKept{}, Source: func(context.Context, string) (Tree, error) { return tr, nil }}
	p, err := s.ScanOnce(context.Background(), "r")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range p.Entries {
		if e.Path == "mcp/x" && (e.Review == nil || e.Review.Verdict != VerdictUnknown) {
			t.Fatalf("mcp/x review = %+v", e.Review)
		}
	}
	if p.Unreviewed != p.Count-1 {
		t.Fatalf("unreviewed %d of %d", p.Unreviewed, p.Count)
	}
}

// Origin: 0.2 map line, 2026-11-20, first review of the 13 repositories with
// no table (one whole-repository entry each): they have no review file of
// their own, so the map reviews them from the file where it lives.
func TestARepositoryWithNoTableIsReviewedFromWhereTheMapLives(t *testing.T) {
	home := tree("api/internal/a/a.go")
	home.Files = append(home.Files, ReviewPath)
	read := home.Read
	home.Read = func(p string) ([]byte, bool, error) {
		if p == ReviewPath {
			return []byte(`{"reviews":{},"repositories":{"other":{".":{"verdict":"claim","domain":"tenant","reason":"r","by":"seat","on":"2026-11-20"}}}}`), true, nil
		}
		return read(p)
	}
	bare := Tree{Commit: "c2", Files: []string{"x.go", "y/z.ts"}, Read: func(string) ([]byte, bool, error) { return nil, false, nil }}
	s := &Scanner{Kept: memKept{}, Source: func(_ context.Context, repo string) (Tree, error) {
		if repo == "home" {
			return home, nil
		}
		return bare, nil
	}}
	if _, err := s.ScanOnce(context.Background(), "home"); err != nil {
		t.Fatal(err)
	}
	p, err := s.ScanOnce(context.Background(), "other")
	if err != nil {
		t.Fatal(err)
	}
	if p.Count != 1 || p.Unreviewed != 0 || p.Entries[0].Review == nil || p.Entries[0].Review.Domain != "tenant" {
		t.Fatalf("other = %+v", p)
	}
	if _, _, err := ParseReviewFile([]byte(`{"repositories":{"a/b":{}}}`)); err == nil {
		t.Fatal("a repository key that is not a repository name must be refused")
	}
	if _, _, err := ParseReviewFile([]byte(`{"repositories":{"r":{".":{"verdict":"delete"}}}}`)); err == nil {
		t.Fatal("a review of another repository with no reason must be refused like any review")
	}
}

func TestRouteReadsOnlyWhatWasKept(t *testing.T) {
	s := &Scanner{Kept: memKept{}, Source: func(context.Context, string) (Tree, error) {
		t.Fatal("the route must not read the source")
		return Tree{}, nil
	}}
	w := httptest.NewRecorder()
	Route{Scanner: s}.ServeHTTP(w, httptest.NewRequest("GET", "/catalog/unperceived?repo=r", nil))
	var p Pool
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil || w.Code != 200 || p.Measured {
		t.Fatalf("code %d body %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	Route{Scanner: s}.ServeHTTP(w, httptest.NewRequest("GET", "/catalog/unperceived?repo=a/b", nil))
	if w.Code != 400 {
		t.Fatalf("bad repo: code %d", w.Code)
	}
}

// 2026-11-20 (0.2 map line, segment 1 entry format): code outside the Go
// packages and the console's groups (mcp/, agent/, scripts/, root files) had
// no row form at all, so it could never leave the pool; path rows give it one.
func TestPathRowsCoverDirectoriesFilesAndTheRoot(t *testing.T) {
	own := `{"packages":{"api/internal/a":{"domains":["plan"]}},"paths":{"mcp/trade":{"domains":["tenant"]},".":{"domains":["foundation"]},"scripts/x.sh":{"domains":["compute"]}}}`
	tab, err := ParseTable([]byte(own), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := paths(Scan(tab, []string{"mcp/trade/src/i.ts", "Makefile", "scripts/x.sh", "scripts/y.sh", "mcp/platform/a.ts"}, nil))
	if len(got) != 2 || got["scripts"] == "" || got["mcp/platform"] == "" {
		t.Fatalf("pool = %v, want only scripts (y.sh) and mcp/platform", got)
	}
	o := ownersOf("r", "c", tab, time.Time{})
	if a := OwnerOf(&o, nil, "mcp/trade/src/i.ts"); !a.Perceived || a.By != "path mcp/trade" || a.Domains[0] != "tenant" {
		t.Fatalf("owner = %+v", a)
	}
	if a := OwnerOf(&o, nil, "Makefile"); !a.Perceived || a.By != "path ." {
		t.Fatalf("root owner = %+v", a)
	}
}

// 2026-11-20 (0.2 map line): the scan covered only the CI scorecard's two
// repositories; it now follows the organization's own listing, so no
// repository name is written into the platform (two flywheels).
func TestRunScansWhatTheListingNames(t *testing.T) {
	seen := make(chan string, 8)
	s := &Scanner{Kept: memKept{}, Source: func(_ context.Context, repo string) (Tree, error) {
		seen <- repo
		return Tree{Commit: "c", Read: func(string) ([]byte, bool, error) { return nil, false, nil }}, nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Run(ctx, func(context.Context) ([]string, error) { return []string{"a", "b"}, nil }, []string{"fallback"})
	got := map[string]bool{<-seen: true, <-seen: true}
	if !got["a"] || !got["b"] {
		t.Fatalf("scanned %v, want a and b from the listing", got)
	}
}
