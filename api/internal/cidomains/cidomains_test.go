package cidomains

import (
	"reflect"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/domainscore"
)

// Origin of every case in this file — 2026-11-16: the Owner read Work → CI and
// could not tell what was running, who depends on whom, or what a change pulls
// in; this page and its two routes replace it. Each case says below what of
// the two answers it guards.

const table = `{
  "domains": [{"id": "foundation"}, {"id": "plan"}, {"id": "owner"}, {"id": "lineage"}],
  "default": {"domains": ["foundation"], "status": "default-maintainer"},
  "frontend": {"root": "console/src"},
  "packages": {
    "api/internal/plan": {"domains": ["plan"], "status": "assigned"},
    "api/internal/plan/view": {"domains": ["plan"], "status": "assigned"},
    "api/internal/owner": {"domains": ["owner"], "status": "assigned"},
    "api/internal/lineage": {"domains": ["lineage"], "status": "assigned"},
    "api/internal/server": {"domains": ["plan", "owner"], "status": "mixed"},
    "api/cmd/tool": {"domains": ["plan"], "status": "assigned"}
  }
}`

func parsed(t *testing.T) Table {
	t.Helper()
	out, err := ParseTable([]byte(table))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func packages() []domainscore.Package {
	return []domainscore.Package{
		{Path: "api/internal/base", Imports: []string{"api/internal/owner"}},
		{Path: "api/internal/base/util"},
		{Path: "api/internal/plan", Imports: []string{"api/internal/base", "api/internal/base/util", "api/internal/lineage", "api/internal/plan/view"}},
		{Path: "api/internal/plan/view", Imports: []string{"api/internal/base", "api/internal/lineage"}},
		{Path: "api/internal/owner", Imports: []string{"api/internal/base"}},
		{Path: "api/internal/lineage", Imports: []string{"api/internal/server"}},
		{Path: "api/internal/server", Imports: []string{"api/internal/plan", "api/internal/owner", "api/internal/base"}},
		{Path: "api/cmd/tool", Imports: []string{"api/internal/server", "api/internal/owner"}},
	}
}

// Origin: as above — guards the graph's shape: a node a declared domain in the
// table's order, an edge counted in package pairs, the shared packages listed
// apart and never a node, and a command (api/cmd) never a package of a domain.
func TestGraphNodesEdgesAndSharedPackages(t *testing.T) {
	got := GraphOf("r", "c", parsed(t).Ownership, packages())
	if got.Version != Version || !got.Measured || got.Repo != "r" || got.Commit != "c" {
		t.Fatalf("header: %+v", got)
	}
	wantDomains := []Domain{{"foundation", 2}, {"plan", 2}, {"owner", 1}, {"lineage", 1}}
	if !reflect.DeepEqual(got.Domains, wantDomains) {
		t.Fatalf("domains: %+v", got.Domains)
	}
	wantEdges := []Edge{
		{"foundation", "owner", 1, true},
		{"plan", "foundation", 3, false},
		{"plan", "lineage", 2, false},
		{"owner", "foundation", 1, false},
	}
	if !reflect.DeepEqual(got.Edges, wantEdges) {
		t.Fatalf("edges: %+v", got.Edges)
	}
	if !reflect.DeepEqual(got.SharedPackages, []string{"api/internal/server"}) {
		t.Fatalf("shared: %v", got.SharedPackages)
	}
}

// Origin: as above — guards "the same rule as the domain score": an edge is
// red on the page exactly when domainscore.Score counts it a cycle edge of
// the importing domain. 2026-11-16: ruler v1 called
// plan -> lineage a cycle through the shared packages; a second copy of the
// rule on the page would have kept doing so.
func TestWrongWayIsTheDomainScoresRule(t *testing.T) {
	own := parsed(t).Ownership
	score := domainscore.Score(domainscore.Input{Ownership: own, Packages: packages(), Ruler: domainscore.Ruler{Version: domainscore.RulerVersion,
		Items: []struct {
			ID       string
			Label    string
			Max      float64
			By       string
			Measured bool
		}{{ID: "dependency_direction", Max: 25}}}})
	counted := map[[2]string]bool{}
	for _, d := range score.Domains {
		for _, it := range d.Items {
			// The cycle edges come first in the evidence; since ruler 3 the
			// cross edges follow them, and those are not red on the page.
			for _, line := range it.Evidence[:int(it.Measures["cycle_edges"])] {
				if _, rest, ok := strings.Cut(line, " -> "); ok {
					to := rest[strings.LastIndex(rest, "(")+1 : len(rest)-1]
					counted[[2]string{d.Domain, to}] = true
				}
			}
		}
	}
	if len(counted) == 0 {
		t.Fatal("positive control: the score counts no wrong-way import in a set that has one")
	}
	for _, e := range GraphOf("r", "c", own, packages()).Edges {
		if e.WrongWay != counted[[2]string{e.From, e.To}] {
			t.Errorf("%s -> %s: page says wrong way %v, the score counts it %v", e.From, e.To, e.WrongWay, counted[[2]string{e.From, e.To}])
		}
	}
}

// Origin: as above — guards "what a change pulls in": a changed file counts
// for the domain of the package it is in (a test's data file for its
// package's), the front end's tree is console, a package several domains
// share is shared, and everything else is the repository's.
func TestPlaceOfAChangedFile(t *testing.T) {
	isPackage := map[string]bool{"api/internal/plan": true, "api/internal/plan/view": true, "api/internal/base": true, "api/internal/server": true}
	p := Places{Table: parsed(t), IsPackage: func(dir string) bool { return isPackage[dir] }}
	for file, want := range map[string]string{
		"api/internal/plan/plan.go":                "plan",
		"api/internal/plan/testdata/deep/a.json":   "plan",
		"api/internal/plan/view/view.go":           "plan",
		"api/internal/base/x.go":                   "foundation",
		"api/internal/newpkg/x.go":                 "foundation", // no row and not a package at the commit read: the default
		"api/internal/owner/gone.go":               "owner",      // its directory is gone at the commit read; the row still says whose it was
		"api/internal/server/server.go":            Shared,
		"api/go.mod":                               "foundation",
		"console/src/pages/shell/ci/CiSection.tsx": Console,
		"console/package.json":                     Console,
		"config/domain-ownership.json":             Repo,
		"README.md":                                Repo,
		"api":                                      Repo,
	} {
		if got := p.Of(file); got != want {
			t.Errorf("%s: %s, want %s", file, got, want)
		}
	}
	got := p.Count([]string{"api/internal/plan/plan.go", "api/internal/plan/view/view.go", "console/a.ts", "Makefile", " "})
	if !reflect.DeepEqual(got, map[string]int{"plan": 2, Console: 1, Repo: 1}) {
		t.Fatalf("count: %v", got)
	}
}

// Origin: as above — guards the changes request: only full commit ids, each
// once, and no more than the page's timeline can ask for in one read.
func TestParseCommits(t *testing.T) {
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	got, err := ParseCommits(a + ", " + b + "," + a + ",")
	if err != nil || !reflect.DeepEqual(got, []string{a, b}) {
		t.Fatalf("got %v %v", got, err)
	}
	for _, bad := range []string{"", ",", "abc", strings.Repeat("A", 40), a + ";" + b} {
		if _, err := ParseCommits(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	var many []string
	for i := 0; i <= MaxCommits; i++ {
		many = append(many, strings.Repeat("0", 38)+string("0123456789abcdef"[i/16])+string("0123456789abcdef"[i%16]))
	}
	if _, err := ParseCommits(strings.Join(many, ",")); err == nil {
		t.Errorf("%d commits were accepted", len(many))
	}
	if got, err := ParseCommits(strings.Join(many[:MaxCommits], ",")); err != nil || len(got) != MaxCommits {
		t.Errorf("%d commits: %d %v", MaxCommits, len(got), err)
	}
}

// Origin: as above — guards "not measured is said, not drawn empty": a table
// that does not read is an error for the caller to report, never zero domains.
func TestParseTableRefusesWhatDoesNotRead(t *testing.T) {
	for _, bad := range []string{"{", `{"domains": []}`} {
		if _, err := ParseTable([]byte(bad)); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}
