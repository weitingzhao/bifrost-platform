package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
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

// Origin: 2026-11-20, chief of staff's acceptance and the perceive-first
// brief: 13 repositories with no table were one entry each, so what is in
// them was not seen. The platform's own are now listed by directory; a
// tenant's (the map reviewed it as tenant's) stays one entry, whole.
func TestARepositoryWithNoTableIsListedByDirectoryUnlessATenants(t *testing.T) {
	none := func(string) ([]byte, bool, error) { return nil, false, nil }
	tr := Tree{Commit: "c", Files: []string{"a", "b/c", "b/d/e"}, Read: none}
	p, _ := poolAndTable("other", tr, false)
	if !p.Measured || len(p.Entries) != 3 || p.Entries[0].Path != "." || p.Entries[1].Path != "b" || p.Entries[2].Path != "b/d" {
		t.Fatalf("by directory: %+v", p.Entries)
	}
	if p.Total != 3 || p.OnMap != 0 || p.OffMap != 3 || p.Gap != 0 || p.Whole {
		t.Errorf("equation %d = %d + %d, gap %d", p.Total, p.OnMap, p.OffMap, p.Gap)
	}
	w, _ := poolAndTable("other", tr, true)
	if !w.Whole || len(w.Entries) != 1 || w.Entries[0].Kind != "repository" || w.Entries[0].Files != 3 || w.Gap != 0 {
		t.Fatalf("whole: %+v", w)
	}
	// Through the scanner: the review that claims the whole repository for
	// tenant keeps it whole; a claim for another domain does not.
	k := memKept{keptOthers: []byte(`{"t":{".":{"verdict":"claim","domain":"payload","reason":"r"}},"u":{".":{"verdict":"claim","domain":"delivery","reason":"r"}}}`)}
	s := &Scanner{Kept: k, Source: func(context.Context, string) (Tree, error) { return tr, nil }}
	if !s.tenantWhole("t") || s.tenantWhole("u") || s.tenantWhole("v") {
		t.Error("tenantWhole")
	}
}

func TestTheEquationHoldsAndCountsWhatTheRowsCover(t *testing.T) {
	own := []byte(`{"packages":{"api/a":{"domains":["plan"]}},"paths":{"docs":{"domains":["plan"]}}}`)
	tr := Tree{Commit: "c", Files: []string{"api/a/x.go", "api/b/y.go", "docs/r.md", "mcp/z.ts", "README.md"},
		Read: func(p string) ([]byte, bool, error) {
			if p == OwnershipPath {
				return own, true, nil
			}
			return nil, false, nil
		}}
	p := PoolOf("r", tr)
	if p.Total != 5 || p.OnMap != 2 || p.OffMap != 3 || p.Gap != 0 {
		t.Fatalf("equation %d = %d + %d, gap %d; entries %+v", p.Total, p.OnMap, p.OffMap, p.Gap, p.Entries)
	}
	// Positive control: a pool that lost an entry no longer adds up.
	p.Entries = p.Entries[1:]
	o := Owners{HasTable: true, GoRoots: []string{"api"}, Packages: map[string][]string{"api/a": {"plan"}}, Paths: map[string][]string{"docs": {"plan"}}}
	p.equation(tr.Files, &o)
	if p.Gap == 0 {
		t.Error("a missing entry left the gap at 0")
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
			return []byte(`{"reviews":{},"repositories":{"other":{".":{"verdict":"claim","domain":"payload","reason":"r","by":"seat","on":"2026-11-20"}}}}`), true, nil
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
	if p.Count != 1 || p.Unreviewed != 0 || p.Entries[0].Review == nil || p.Entries[0].Review.Domain != "payload" {
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

// 2026-11-21, the Owner asked how the map knows a thing was removed: it saw
// additions (the pool) but a row whose thing was deleted stayed registered,
// unseen. The control: a row that covers a file is not stale.
func TestARowThatCoversNothingIsStale(t *testing.T) {
	table := Table{Packages: map[string]bool{"api/internal/a": true, "api/internal/gone": true},
		Groups: []string{"console/src/live", "console/src/old"}, Paths: []string{"scripts", "docs", "."}, Domains: map[string][]string{}}
	files := []string{"api/internal/a/a.go", "api/internal/gone/sub/x.go", "console/src/live/p.tsx", "scripts/s.sh", "go.mod"}
	got := staleRows(table, files)
	want := []string{"api/internal/gone", "console/src/old", "docs"}
	if len(got) != len(want) {
		t.Fatalf("stale %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stale %v, want %v", got, want)
		}
	}
	if s := staleRows(table, []string{"api/internal/a/a.go"}); len(s) != 6 {
		t.Errorf("with no root file the root row is stale too: %v", s)
	}
}

// 2026-11-21, main line: node was a registry-only class. topology.yaml had
// said ubt-k3s-06 was in the cluster for weeks after it left (topologyview);
// the map now compares the claims with the node list each round. The
// control: a machine claimed and live is in neither list; one not claimed as
// a cluster node (in_k3s_cluster false) is not a claim.
func TestNodesAreComparedWithWhatTopologyClaims(t *testing.T) {
	s := &Scanner{Kept: memKept{}}
	s.keepTopology(Tree{Commit: "c", Read: func(p string) ([]byte, bool, error) {
		return []byte(`nodes:
  - {id: a, host: 10.0.0.1, in_k3s_cluster: true}
  - {id: gone, host: 10.0.0.2, in_k3s_cluster: true}
  - {id: mac, host: 10.0.0.9, in_k3s_cluster: false}
  - {id: edge, host: "", in_k3s_cluster: true}
`), true, nil
	}})
	c := s.nodeCheck(context.Background(), func(context.Context) (map[string]string, bool) {
		return map[string]string{"10.0.0.1": "node-a", "10.0.0.3": "node-new"}, true
	})
	if !c.Measured || c.Registered != 2 || c.Live != 2 {
		t.Fatalf("%+v", c)
	}
	if len(c.NotRegistered) != 1 || c.NotRegistered[0] != "node-new (10.0.0.3)" || len(c.NotLive) != 1 || c.NotLive[0] != "gone (10.0.0.2)" {
		t.Errorf("%+v", c)
	}
	if c := s.nodeCheck(context.Background(), func(context.Context) (map[string]string, bool) { return nil, false }); c.Measured {
		t.Errorf("an unread node list is not measured: %+v", c)
	}
}

// 2026-11-21, Crew's measure for class seat (charters/seats.yaml): it was
// run by hand (check-seats.py), so the map could not show a page with no
// row, a row with no page, a domain id the table lacks, or a temporary seat
// nobody invited. The repository comes from the index, never from code. The
// control: a page named by one row with a known domain raises nothing.
func TestSeatsAreComparedWithTheirCharterPages(t *testing.T) {
	s := &Scanner{Kept: memKept{}}
	ix, _ := json.Marshal(Index{Classes: []Class{{Class: "seat", Sources: []string{"infra:c/seats.yaml"}, Check: CheckScanned}}})
	_ = s.kept().Write(keptIndex, ix)
	tree := Tree{Commit: "c", Files: []string{"c/seats.yaml", "c/README.md", "c/a.md", "c/b.md", "c/new.md"},
		Read: func(p string) ([]byte, bool, error) {
			return []byte(`seats:
  - {id: a, charter: a.md, domain_id: plan, kind: owner}
  - {id: b, charter: b.md, domain_id: nowhere, kind: owner}
  - {id: gone, charter: gone.md, domain_id: null, kind: owner}
temporary:
  - {id: visit, kind: temporary}
`), true, nil
		}}
	s.keepSeats("other", tree)
	if c := s.seatCheck(nil); c.Measured {
		t.Fatalf("a repository the index does not name is not read: %+v", c)
	}
	s.keepSeats("infra", tree)
	c := s.seatCheck([]contractDomain{{ID: "plan"}})
	if !c.Measured || c.Registered != 4 || c.Live != 3 {
		t.Fatalf("%+v", c)
	}
	if len(c.NotRegistered) != 1 || c.NotRegistered[0] != "new.md" || len(c.NotLive) != 1 || c.NotLive[0] != "gone (gone.md)" {
		t.Errorf("%+v", c)
	}
	if len(c.Problems) != 2 {
		t.Errorf("want the unknown domain and the uninvited temporary: %+v", c.Problems)
	}
}

// 2026-11-22, the compute line and Foundation: MCP tools were in no class
// of the map; 55 of 132 registered tools were in no domain's contract
// (temporary reading, main 73eec5d0). The control: a tool both registered
// and in a contract is in neither list, and a test file's reg() is no tool.
func TestMCPToolsAreComparedWithTheContracts(t *testing.T) {
	s := &Scanner{Kept: memKept{}}
	src := map[string]string{
		"mcp/platform/src/index.ts":      "reg(\n  'get_domain', 'd', {}, f)\nreg('get_new', 'n', {}, f)",
		"mcp/unifi/src/tools.ts":         "server.tool('apply_firewall', 'x')",
		"mcp/platform/src/index.test.ts": "reg('only_in_test', 'x')",
	}
	files := []string{}
	for f := range src {
		files = append(files, f)
	}
	s.keepMCPTools(Tree{Commit: "c", Files: files, Read: func(p string) ([]byte, bool, error) { return []byte(src[p]), true, nil }})
	var domains []contractDomain
	if err := json.Unmarshal([]byte(`[{"id":"foundation","contracts":{"commands":[{"kind":"mcp_tool","name":"get_domain"},{"kind":"mcp_tool","name":"get_gone"},{"kind":"action","name":"x"}]}}]`), &domains); err != nil {
		t.Fatal(err)
	}
	c := s.mcpToolCheck(domains)
	if !c.Measured || c.Live != 3 || c.Registered != 2 {
		t.Fatalf("%+v", c)
	}
	if len(c.NotRegistered) != 2 || c.NotRegistered[0] != "apply_firewall (unifi)" || len(c.NotLive) != 1 || c.NotLive[0] != "get_gone" {
		t.Errorf("%+v", c)
	}
}

// 2026-11-22, the compute line: bifrost_node:home_info and the ComputePain
// alerts were in no class of the map. The control: a group declared and
// loaded is in neither list; a non-PrometheusRule document is no group.
func TestRuleGroupsAreComparedWithWhatPrometheusLoaded(t *testing.T) {
	s := &Scanner{Kept: memKept{}}
	ix, _ := json.Marshal(Index{Classes: []Class{{Class: "prometheus-rule", Sources: []string{"infra:k8s/monitoring/"}, Check: CheckScanned}}})
	_ = s.kept().Write(keptIndex, ix)
	src := map[string]string{
		"k8s/monitoring/compute.yaml": "kind: PrometheusRule\nspec:\n  groups:\n    - name: compute\n      rules:\n        - record: bifrost_node:home_info\n        - alert: ComputePainDbDiskFlush\n---\nkind: ConfigMap\nspec:\n  groups:\n    - name: not-a-rule\n",
		"k8s/monitoring/gone.yaml":    "kind: PrometheusRule\nspec:\n  groups:\n    - name: gone\n      rules:\n        - alert: X\n",
		"k8s/other/x.yaml":            "kind: PrometheusRule\nspec:\n  groups:\n    - name: elsewhere\n",
	}
	files := []string{}
	for f := range src {
		files = append(files, f)
	}
	s.keepRules("infra", Tree{Commit: "c", Files: files, Read: func(p string) ([]byte, bool, error) { return []byte(src[p]), true, nil }})
	c := s.ruleCheck(context.Background(), func(context.Context) ([]RuleGroup, bool) {
		return []RuleGroup{{Name: "compute", Rules: []string{"a", "b"}}, {Name: "kubernetes-apps", Rules: []string{"x"}}}, true
	})
	if !c.Measured || c.Registered != 2 || c.Live != 2 {
		t.Fatalf("%+v", c)
	}
	if len(c.NotRegistered) != 1 || c.NotRegistered[0] != "kubernetes-apps (1 rules)" || len(c.NotLive) != 1 || c.NotLive[0] != "gone (k8s/monitoring/gone.yaml)" {
		t.Errorf("%+v", c)
	}
}

// 2026-11-22, Foundation: the front end's root group (console/src) catches
// every file no narrower group names, so a new directory nobody declared
// reads as owned. The map counts what only the root group covers. The
// control: a file under a narrower group is not counted.
func TestFilesOnlyTheRootGroupCoversAreCounted(t *testing.T) {
	table := Table{ConsoleRoot: "console/src", Groups: []string{"console/src/pages", "console/src"}}
	files := []string{"console/src/main.tsx", "console/src/hooks/a.ts", "console/src/hooks/b.ts", "console/src/pages/p.tsx", "api/x.go"}
	got := rootGroupOnly(table, files)
	if len(got) != 2 || got[0] != "console/src/(files) (1)" || got[1] != "console/src/hooks (2)" {
		t.Fatalf("%v", got)
	}
	if got := rootGroupOnly(Table{ConsoleRoot: "console/src", Groups: []string{"console/src/pages"}}, files); len(got) != 0 {
		t.Errorf("with no root group nothing is caught by it: %v", got)
	}
}

// 2026-11-22, law 75 (the map is the counter: "I am adding this, the map adds
// it"): a path no row covers gets the nearest owned directory above it and
// the row to put in the same commit, its domain the nearest's by default or
// the one asked for. The control: a covered path gets no claim.
func TestAnUnownedPathGetsTheRowThatClaimsIt(t *testing.T) {
	s := &Scanner{Kept: memKept{}}
	o := Owners{Repo: "r", Commit: "c", HasTable: true, GoRoots: []string{"api"}, Console: "console",
		Packages: map[string][]string{"api/internal/a": {"plan"}}, Groups: map[string][]string{},
		Paths: map[string][]string{"scripts": {"foundation"}}}
	raw, _ := json.Marshal(o)
	_ = s.kept().Write(keptOwners("r"), raw)
	get := func(q string) Owner {
		w := httptest.NewRecorder()
		OwnerRoute{Scanner: s}.ServeHTTP(w, httptest.NewRequest("GET", "/catalog/owner?repo=r&"+q, nil))
		var out Owner
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		return out
	}
	if c := get("path=scripts/x.sh"); c.Claim != nil || !c.Perceived {
		t.Fatalf("covered path: %+v", c)
	}
	u := get("path=tools/new/run.py")
	if u.Perceived || u.Claim == nil || u.Claim.Section != "paths" || u.Claim.Key != "tools/new/run.py" || !strings.Contains(string(u.Claim.Row), "{domain}") || u.Nearest != nil {
		t.Fatalf("no owner above: %+v %s", u.Claim, u.Claim.Row)
	}
	g := get("path=api/cmd/tool/main.go&domain=lineage")
	if g.Claim == nil || g.Claim.Section != "packages" || g.Claim.Key != "api/cmd/tool" || !strings.Contains(string(g.Claim.Row), `"lineage"`) {
		t.Fatalf("new package: %+v", g.Claim)
	}
	if !strings.Contains(g.Claim.Say, "same commit") {
		t.Errorf("say: %q", g.Claim.Say)
	}
}

// 2026-11-22: event was a registry-only class; the map compares the declared
// types with what the ledger wrote in seven days. The control: a declared
// type that was written is in neither list.
func TestEventTypesAreComparedWithWhatTheLedgerWrote(t *testing.T) {
	c := eventCheck(context.Background(), func(context.Context) (map[string]int, bool) {
		return map[string]int{"approval.card.created": 3, "made.up.type": 1}, true
	})
	if !c.Measured || len(c.NotRegistered) != 1 || c.NotRegistered[0] != "made.up.type (1 in 7 days)" {
		t.Fatalf("%+v", c.NotRegistered)
	}
	for _, s := range c.NotLive {
		if s == "approval.card.created" {
			t.Errorf("a written type listed as silent")
		}
	}
	if len(c.NotLive) == 0 || c.Registered < 2 {
		t.Errorf("%+v", c)
	}
	if c := eventCheck(context.Background(), nil); c.Measured {
		t.Errorf("no ledger is not measured")
	}
}
