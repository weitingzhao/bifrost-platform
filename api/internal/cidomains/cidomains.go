// Package cidomains is what the CI page (Work > CI) reads about a
// repository's domains: who imports whom, and which domains a commit changed.
// It is a reading: it decides nothing and gates nothing.
//
// Both answers are contracts with a version in the body (Version). The domain
// of a package and the direction of an import are domainscore's (Graph): the
// page cannot draw an edge red that the domain score does not count.
package cidomains

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/domainscore"
)

// Version is the version of both answers' bodies.
const Version = 1

// OwnershipPath is the table a repository declares its domains in.
const OwnershipPath = "config/domain-ownership.json"

// The three places of a changed file that are not a declared domain.
const (
	// Shared is a package whose row names several domains.
	Shared = "shared"
	// Console is the front end's tree.
	Console = "console"
	// Repo is every other file of the repository.
	Repo = "repo"
)

// MaxCommits is how many commits one changes request may name.
const MaxCommits = 120

// Table is config/domain-ownership.json as far as this package reads it.
type Table struct {
	domainscore.Ownership
	Frontend struct{ Root string }
}

// ParseTable reads the ownership table.
func ParseTable(raw []byte) (Table, error) {
	var t Table
	if err := json.Unmarshal(raw, &t); err != nil {
		return t, fmt.Errorf("the ownership table does not parse: %w", err)
	}
	if len(t.Domains) == 0 {
		return t, fmt.Errorf("the ownership table declares no domain")
	}
	return t, nil
}

// GoRoots are the top directories the table's package rows are under: where
// its Go code is ("api" for rows like api/internal/x).
func (t Table) GoRoots() []string {
	seen := map[string]bool{}
	var out []string
	for p := range t.Packages {
		if root, _, _ := strings.Cut(p, "/"); root != "" && !seen[root] {
			seen[root], out = true, append(out, root)
		}
	}
	sort.Strings(out)
	return out
}

// consoleRoot is the top directory of the front end ("console" for a root of
// console/src), "" when the table declares none.
func (t Table) consoleRoot() string {
	root, _, _ := strings.Cut(strings.Trim(t.Frontend.Root, "/"), "/")
	return root
}

type Domain struct {
	ID       string `json:"id"`
	Packages int    `json:"packages"`
}

// Edge is the imports of one domain's packages from another's: Imports
// counts the pairs (importing package, imported package).
type Edge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Imports  int    `json:"imports"`
	WrongWay bool   `json:"wrong_way"`
	// Pairs are the imports themselves, importer then imported, sorted: the
	// package layer under the edge. Imports is their number.
	Pairs []Pair `json:"pairs"`
}

// Graph is the answer of GET /api/v1/ci/domains.
// Pair is one import: the package that uses and the package it uses.
type Pair struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type Graph struct {
	Version int    `json:"version"`
	Repo    string `json:"repo"`
	// Commit is the commit this graph was read at. Head is main's head when
	// the answer was given; while they differ the graph of Head is being read
	// (Refreshing) and this one is the last that was.
	Commit     string `json:"commit"`
	Head       string `json:"head"`
	Refreshing bool   `json:"refreshing"`
	// Measured is false for a repository with no ownership table (or one that
	// does not read); Note says which.
	Measured bool   `json:"measured"`
	Note     string `json:"note"`
	// Base is the domain every other stands on (the ownership table's
	// default), "" when the table names none: an import of it points the
	// right way, an import by it of a domain that stands on it the wrong way.
	Base    string   `json:"base"`
	Domains []Domain `json:"domains"`
	Edges   []Edge   `json:"edges"`
	// SharedPackages are the packages several domains share. They are not a
	// node: they import every domain, and an edge through them joins any two.
	SharedPackages []string `json:"shared_packages"`
}

// Unmeasured is the answer for a repository whose domains cannot be read.
func Unmeasured(repo, commit, note string) Graph {
	return Graph{Version: Version, Repo: repo, Commit: commit, Note: note, Domains: []Domain{}, Edges: []Edge{}, SharedPackages: []string{}}
}

// GraphOf is the domain graph of the packages: a node a declared domain, an
// edge the imports between two of them.
func GraphOf(repo, commit string, own domainscore.Ownership, packages []domainscore.Package) Graph {
	g := domainscore.NewGraph(own, packages)
	out := Unmeasured(repo, commit, "")
	out.Measured = true
	count := map[string]int{}
	edges := map[[2]string][]Pair{}
	for _, p := range g.Packages() {
		from, _ := g.DomainOf(p)
		if from == domainscore.Mixed {
			out.SharedPackages = append(out.SharedPackages, p)
			continue
		}
		count[from]++
		for _, dep := range g.Imports(p) {
			if to, _ := g.DomainOf(dep); to != from && to != domainscore.Mixed {
				edges[[2]string{from, to}] = append(edges[[2]string{from, to}], Pair{From: p, To: dep})
			}
		}
	}
	at := map[string]int{}
	for i, d := range g.Order {
		at[d] = i
		out.Domains = append(out.Domains, Domain{ID: d, Packages: count[d]})
	}
	for k, pairs := range edges {
		sort.Slice(pairs, func(i, j int) bool {
			if pairs[i].From != pairs[j].From {
				return pairs[i].From < pairs[j].From
			}
			return pairs[i].To < pairs[j].To
		})
		out.Edges = append(out.Edges, Edge{From: k[0], To: k[1], Imports: len(pairs), WrongWay: g.WrongWay(k[0], k[1]), Pairs: pairs})
	}
	out.Base = g.Base()
	sort.Slice(out.Edges, func(i, j int) bool {
		a, b := out.Edges[i], out.Edges[j]
		if a.From != b.From {
			return at[a.From] < at[b.From]
		}
		return at[a.To] < at[b.To]
	})
	return out
}

// Places says which domain a changed file is in, by the ownership table and
// the directories that are Go packages at the commit the table was read at.
type Places struct {
	Table Table
	// IsPackage says a directory (from the repository root) holds a Go package.
	IsPackage func(dir string) bool
}

// Of is the place of one file: Console under the front end's tree; under the
// Go code the domain of the nearest directory above it that is a package
// (Shared for a package of several domains; the default domain for a file in
// no package, such as go.mod); Repo for everything else.
func (p Places) Of(file string) string {
	file = strings.Trim(path.Clean(file), "/")
	root, _, _ := strings.Cut(file, "/")
	if console := p.Table.consoleRoot(); console != "" && root == console {
		return Console
	}
	inGo := false
	for _, r := range p.Table.GoRoots() {
		inGo = inGo || r == root
	}
	if !inGo || !strings.Contains(file, "/") {
		return Repo
	}
	dir := path.Dir(file)
	for strings.Contains(dir, "/") && !(p.IsPackage != nil && p.IsPackage(dir)) {
		// A row is a package the table knows even when the directory is gone
		// at the commit read (a commit that deleted or predates it).
		if _, row := p.Table.Packages[dir]; row {
			break
		}
		dir = path.Dir(dir)
	}
	domain, _ := p.Table.DomainOf(dir)
	if domain == domainscore.Mixed {
		return Shared
	}
	return domain
}

// Count is how many of the files are in each place.
func (p Places) Count(files []string) map[string]int {
	out := map[string]int{}
	for _, f := range files {
		if strings.TrimSpace(f) != "" {
			out[p.Of(f)]++
		}
	}
	return out
}

// Change is what one commit changed against its first parent.
type Change struct {
	Commit string `json:"commit"`
	// Read is false for a commit the repository does not have (one that never
	// reached a kept branch) or whose files could not be read; Domains is
	// then empty, which is not "changed nothing".
	Read    bool           `json:"read"`
	Domains map[string]int `json:"domains"`
}

// Changes is the answer of GET /api/v1/ci/changes.
type Changes struct {
	Version int    `json:"version"`
	Repo    string `json:"repo"`
	// Measured is false for a repository with no ownership table: there is no
	// domain to count a file for, and Changes is empty. Note says why.
	Measured bool     `json:"measured"`
	Note     string   `json:"note"`
	Changes  []Change `json:"changes"`
}

var commitID = regexp.MustCompile(`^[0-9a-f]{40}$`)

// ParseCommits reads the commits parameter: full commit ids between commas,
// each once, at most MaxCommits.
func ParseCommits(value string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, c := range strings.Split(value, ",") {
		if c = strings.TrimSpace(c); c == "" {
			continue
		}
		if !commitID.MatchString(c) {
			return nil, fmt.Errorf("commits are 40-character lowercase SHAs between commas")
		}
		if !seen[c] {
			seen[c], out = true, append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("commits names no commit")
	}
	if len(out) > MaxCommits {
		return nil, fmt.Errorf("commits names %d commits; at most %d", len(out), MaxCommits)
	}
	return out, nil
}
