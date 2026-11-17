package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/cidomains"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

// Origin of every case in this file — 2026-11-16: the Owner read Work → CI and
// could not tell what was running, who depends on whom, or what a change pulls
// in; this page and its two routes replace it. Each case says below what of
// the two routes it guards.

const ciDomainsTable = `{
  "domains": [{"id": "foundation"}, {"id": "plan"}, {"id": "owner"}],
  "default": {"domains": ["foundation"], "status": "default-maintainer"},
  "frontend": {"root": "console/src"},
  "packages": {
    "api/internal/plan": {"domains": ["plan"], "status": "assigned"},
    "api/internal/owner": {"domains": ["owner"], "status": "assigned"},
    "api/internal/server": {"domains": ["plan", "owner"], "status": "mixed"}
  }
}`

func ciDomainsFiles() map[string]string {
	const mod = "example.com/p/api"
	return map[string]string{
		"config/domain-ownership.json":      ciDomainsTable,
		"api/go.mod":                        "module " + mod + "\n\ngo 1.25\n",
		"api/internal/base/base.go":         "package base\n",
		"api/internal/acts/acts.go":         "package acts\n\nimport _ \"" + mod + "/internal/owner\"\n",
		"api/internal/plan/plan.go":         "package plan\n\nimport (\n\t_ \"" + mod + "/internal/base\"\n\t_ \"fmt\"\n)\n",
		"api/internal/plan/plan_test.go":    "package plan\n\nimport _ \"" + mod + "/internal/owner\"\n",
		"api/internal/plan/testdata/a.json": "{}",
		"api/internal/owner/owner.go":       "package owner\n\nimport _ \"" + mod + "/internal/base\"\n",
		"api/internal/server/server.go":     "package server\n\nimport (\n\t_ \"" + mod + "/internal/plan\"\n\t_ \"" + mod + "/internal/owner\"\n)\n",
		"console/src/a.ts":                  "export {}\n",
	}
}

// memKept is a ciKept in memory: what one cache wrote, another reads.
type memKept struct {
	mu   sync.Mutex
	data map[string][]byte
}

func (m *memKept) Read(name string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.data[name], nil
}

func (m *memKept) Write(name string, raw []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data == nil {
		m.data = map[string][]byte{}
	}
	m.data[name] = append([]byte(nil), raw...)
	return nil
}

func ciDomainsSrc(t *testing.T, head *string, trees map[string]map[string]string, files func(ctx context.Context, repo, commit string) ([]string, error)) (ciDomainsSource, *atomic.Int32) {
	t.Helper()
	read := scopeTrees(t, trees)
	var treeReads atomic.Int32
	return ciDomainsSource{
		head: func(context.Context, string) (string, error) { return *head, nil },
		tree: func(ctx context.Context, repo, commit string) (*unitplan.Tree, error) {
			treeReads.Add(1)
			return read(ctx, repo, commit)
		},
		files: files,
	}, &treeReads
}

// Origin: as above — guards the graph route's reading: the imports are read
// from the Go files of main's tree (no checkout, no go command), production
// imports only (the test's import of owner is not an edge), and the answer is
// kept while main stays at that commit and read again when it moves.
func TestCIDomainsReadsTheTreeAndKeepsItByCommit(t *testing.T) {
	head := "c1"
	moved := ciDomainsFiles()
	moved["api/internal/plan/plan.go"] = "package plan\n\nimport (\n\t_ \"example.com/p/api/internal/base\"\n\t_ \"example.com/p/api/internal/owner\"\n)\n"
	src, reads := ciDomainsSrc(t, &head, map[string]map[string]string{"c1": ciDomainsFiles(), "c2": moved}, nil)
	cache := ciDomainsCache{kept: &memKept{}}
	got, err := cache.graph(context.Background(), src, "r")
	if err != nil {
		t.Fatal(err)
	}
	var edges []string
	for _, e := range got.Edges {
		edges = append(edges, fmt.Sprintf("%s>%s %d %v %d", e.From, e.To, e.Imports, e.WrongWay, len(e.Pairs)))
	}
	want := []string{"foundation>owner 1 true 1", "plan>foundation 1 false 1", "owner>foundation 1 false 1"}
	if got.Version != 1 || !got.Measured || got.Commit != "c1" || got.Base != "foundation" || !reflect.DeepEqual(edges, want) || !reflect.DeepEqual(got.SharedPackages, []string{"api/internal/server"}) {
		t.Fatalf("graph: %+v", got)
	}
	if !reflect.DeepEqual(got.Domains, []cidomains.Domain{{ID: "foundation", Packages: 2}, {ID: "plan", Packages: 1}, {ID: "owner", Packages: 1}}) {
		t.Fatalf("domains: %+v", got.Domains)
	}
	if _, err = cache.graph(context.Background(), src, "r"); err != nil || reads.Load() != 1 {
		t.Fatalf("a second read at the same commit read the tree again: %d reads, %v", reads.Load(), err)
	}
	// Origin: 2026-11-16 22:5x, the Owner switched to By dependency and saw "loading" for a long
	// time: main had moved, the platform had restarted, and the look itself began the read of
	// every Go file. When main moves the last graph is answered at once, marked with the commit
	// it is of and that a newer one is being read; the read happens away from the request.
	head = "c2"
	got, err = cache.graph(context.Background(), src, "r")
	if err != nil || got.Commit != "c1" || got.Head != "c2" || !got.Refreshing {
		t.Fatalf("main moved, first answer: %+v, %v", got, err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; {
		got, err = cache.graph(context.Background(), src, "r")
		if err != nil || got.Commit == "c2" || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err != nil || reads.Load() != 2 || got.Commit != "c2" || got.Head != "c2" || got.Refreshing {
		t.Fatalf("main moved, once read: %d reads, %+v, %v", reads.Load(), got, err)
	}
	// A process that starts later (the platform restarts at every delivery) has the graph from
	// what was kept, without a read.
	restarted := ciDomainsCache{kept: cache.kept}
	if again, err := restarted.graph(context.Background(), src, "r"); err != nil || again.Commit != "c2" || again.Refreshing || reads.Load() != 2 {
		t.Fatalf("after a restart: %d reads, %+v, %v", reads.Load(), again, err)
	}
	edge := func(e cidomains.Edge) string { return fmt.Sprintf("%s>%s %d %v", e.From, e.To, e.Imports, e.WrongWay) }
	if len(got.Edges) != 4 || edge(got.Edges[1]) != "plan>foundation 1 false" || edge(got.Edges[2]) != "plan>owner 1 false" || len(got.Edges[1].Pairs) != 1 {
		t.Fatalf("edges after the move: %+v", got.Edges)
	}
}

// Origin: as above — guards "not measured is said": a repository with no
// ownership table (bifrost-trade-infra today) answers measured false with a
// note, not an empty graph that reads as "no dependencies"; and a read that
// failed is an error, never kept.
func TestCIDomainsWithoutATableIsUnmeasuredAndAFailedReadIsNotKept(t *testing.T) {
	head := "c1"
	plain := map[string]string{"README.md": "x\n"}
	src, _ := ciDomainsSrc(t, &head, map[string]map[string]string{"c1": plain}, nil)
	var cache ciDomainsCache
	got, err := cache.graph(context.Background(), src, "r")
	if err != nil || got.Measured || !strings.Contains(got.Note, "config/domain-ownership.json") || got.Domains == nil || got.Edges == nil || got.SharedPackages == nil {
		t.Fatalf("no table: %+v %v", got, err)
	}
	changes, err := cache.changes(context.Background(), src, time.Now(), "r", []string{strings.Repeat("a", 40)})
	if err != nil || changes.Measured || changes.Note == "" || len(changes.Changes) != 0 {
		t.Fatalf("changes with no table: %+v %v", changes, err)
	}
	fail := true
	src.tree = func(ctx context.Context, repo, commit string) (*unitplan.Tree, error) {
		if fail {
			return nil, errors.New("gitea: HTTP 502")
		}
		return scopeTrees(t, map[string]map[string]string{"c1": ciDomainsFiles()})(ctx, repo, commit)
	}
	var second ciDomainsCache
	if _, err = second.graph(context.Background(), src, "r"); err == nil {
		t.Fatal("a failed read answered")
	}
	fail = false
	if got, err = second.graph(context.Background(), src, "r"); err != nil || !got.Measured {
		t.Fatalf("after the failure: %+v %v", got, err)
	}
}

// Origin: as above — guards the changes route: files are counted by the
// domain of their package at main's head, a commit the repository does not
// have is read false (2026-11-16: 21 of 102 runs were of commits that never
// reached main; they must not read as "changed nothing"), a read commit is
// kept for good and an unread one is asked again only after a while.
func TestCIChangesCountsByDomainAndKeepsWhatItRead(t *testing.T) {
	head := "c1"
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	var asked atomic.Int32
	files := func(_ context.Context, _, commit string) ([]string, error) {
		asked.Add(1)
		if commit == b {
			return nil, errors.New("gitea: HTTP 404")
		}
		return []string{"api/internal/plan/plan.go", "api/internal/plan/testdata/a.json", "api/internal/server/server.go", "api/internal/base/base.go", "console/src/a.ts", "Makefile"}, nil
	}
	src, _ := ciDomainsSrc(t, &head, map[string]map[string]string{"c1": ciDomainsFiles()}, files)
	cache := ciDomainsCache{kept: &memKept{}}
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	got, err := cache.changes(context.Background(), src, now, "r", []string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	want := cidomains.Changes{Version: 1, Repo: "r", Measured: true, Changes: []cidomains.Change{
		{Commit: a, Read: true, Domains: map[string]int{"plan": 2, "shared": 1, "foundation": 1, "console": 1, "repo": 1}},
		{Commit: b, Read: false, Domains: map[string]int{}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changes: %+v", got)
	}
	if _, err = cache.changes(context.Background(), src, now.Add(time.Minute), "r", []string{a, b}); err != nil || asked.Load() != 2 {
		t.Fatalf("a minute later: asked %d times, %v", asked.Load(), err)
	}
	if _, err = cache.changes(context.Background(), src, now.Add(ciChangesRetry+time.Second), "r", []string{a, b}); err != nil || asked.Load() != 3 {
		t.Fatalf("after the retry time only the unread commit is asked again: asked %d times, %v", asked.Load(), err)
	}
	// 2026-11-16: after a restart the page asked Gitea again for every commit it shows. What was
	// read is kept: a later process asks only for the commit that never read.
	restarted := ciDomainsCache{kept: cache.kept}
	again, err := restarted.changes(context.Background(), src, now.Add(time.Hour), "r", []string{a, b})
	if err != nil || asked.Load() != 4 || !reflect.DeepEqual(again, want) {
		t.Fatalf("after a restart: asked %d times (want 4: only the unread one), %+v, %v", asked.Load(), again, err)
	}
}

// Origin: as above — guards the two routes' requests: a repository name and
// full commit ids only, refused before anything is read.
func TestCIDomainRoutesRefuseBadRequests(t *testing.T) {
	s := &Server{}
	for _, c := range []struct {
		handler http.HandlerFunc
		query   string
		status  int
	}{
		{s.handleCIDomains, "", http.StatusBadRequest},
		{s.handleCIDomains, "repo=a/b", http.StatusBadRequest},
		{s.handleCIDomains, "repo=r", http.StatusServiceUnavailable},
		{s.handleCIChanges, "repo=r", http.StatusBadRequest},
		{s.handleCIChanges, "repo=r&commits=abc", http.StatusBadRequest},
		{s.handleCIChanges, "repo=x.git&commits=" + strings.Repeat("a", 40), http.StatusBadRequest},
		{s.handleCIChanges, "repo=r&commits=" + strings.Repeat("a", 40), http.StatusServiceUnavailable},
	} {
		w := httptest.NewRecorder()
		c.handler(w, httptest.NewRequest(http.MethodGet, "/?"+c.query, nil))
		if w.Code != c.status {
			t.Errorf("%q: HTTP %d, want %d", c.query, w.Code, c.status)
		}
	}
}
