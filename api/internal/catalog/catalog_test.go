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
	if _, err := ParseReviews([]byte(`{"reviews":{"mcp/x":{"verdict":"claim"}}}`)); err == nil {
		t.Fatal("a claim verdict must be refused: a claimed entry becomes a row")
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
