// Package domainscore scores each domain's code against config/quality-ruler.json:
// one ruler for every domain, a version on every score, so that a domain's
// number can be compared with its own earlier numbers (ISSUE-20).
//
// It is a reading: it decides nothing and gates nothing. Version 4 measures
// dependency direction and, of the contracts item, the Go surface (5 of its
// 20 points, contracts.go); everything else is reported unmeasured.
package domainscore

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// RulerVersion is the ruler version whose formulas this code computes. The
// ruler file must say the same number.
const RulerVersion = 4

// Mixed is the domain of a package whose row names several domains.
const Mixed = "mixed"

const assemblyRoots = "api/cmd/"

// Ownership is config/domain-ownership.json as far as the score reads it.
type Ownership struct {
	Domains []struct{ ID string }
	Default struct {
		Domains []string
		Status  string
	}
	Packages map[string]struct {
		Domains []string
		Status  string
	}
}

// DomainOf is the one domain of a package: its row's, the default's for a
// package with no row, and Mixed for several. assigned says the row itself
// says so.
func (o Ownership) DomainOf(pkg string) (domain string, assigned bool) {
	domains, status := o.Default.Domains, o.Default.Status
	if row, ok := o.Packages[pkg]; ok && len(row.Domains) > 0 {
		domains, status = row.Domains, row.Status
	}
	if len(domains) != 1 {
		return Mixed, false
	}
	return domains[0], status == "assigned"
}

// Ruler is config/quality-ruler.json.
type Ruler struct {
	Version int
	Items   []RulerItem
	Walls   map[string]struct{ File, Test string }
}

// RulerItem is one item of the ruler. MeasuredMax is how many of its Max
// points this version measures: all of them when the ruler does not say, and
// fewer when only some of its Parts are measured.
type RulerItem struct {
	ID          string
	Label       string
	Max         float64
	By          string
	Measured    bool
	MeasuredMax float64 `json:"measured_max"`
	Parts       []struct {
		ID       string
		Max      float64
		Measured bool
	}
}

// ParseRuler reads the ruler and refuses one this code does not compute.
func ParseRuler(raw []byte) (Ruler, error) {
	var r Ruler
	if err := json.Unmarshal(raw, &r); err != nil {
		return r, fmt.Errorf("the ruler does not parse: %w", err)
	}
	if r.Version != RulerVersion {
		return r, fmt.Errorf("the ruler is version %d and this code computes version %d", r.Version, RulerVersion)
	}
	total := 0.0
	for i, it := range r.Items {
		total += it.Max
		if it.Measured && it.MeasuredMax == 0 {
			r.Items[i].MeasuredMax = it.Max
		}
		if !it.Measured && it.MeasuredMax != 0 || it.MeasuredMax > it.Max {
			return r, fmt.Errorf("the ruler's item %s measures %v of %v points (measured=%v)", it.ID, it.MeasuredMax, it.Max, it.Measured)
		}
		if len(it.Parts) > 0 {
			all, measured := 0.0, 0.0
			for _, p := range it.Parts {
				all += p.Max
				if p.Measured {
					measured += p.Max
				}
			}
			if all != it.Max || measured != r.Items[i].MeasuredMax {
				return r, fmt.Errorf("the parts of the ruler's item %s add up to %v with %v measured; the item says %v and %v", it.ID, all, measured, it.Max, r.Items[i].MeasuredMax)
			}
		}
		if it.ID == "contracts" && it.Measured && r.Items[i].MeasuredMax != GoSurfaceMax {
			return r, fmt.Errorf("the ruler measures %v points of contracts and this code computes %d", r.Items[i].MeasuredMax, GoSurfaceMax)
		}
	}
	if total != 100 || len(r.Items) == 0 {
		return r, fmt.Errorf("the ruler's items add up to %v, not 100", total)
	}
	return r, nil
}

// Package is one package of the module: its path from the repository root,
// the packages of the module its production code imports, and how many
// lines its production files have.
type Package struct {
	Path    string
	Imports []string
	Lines   int
}

// Input is everything a score is computed from.
type Input struct {
	Commit    string
	Ruler     Ruler
	Ownership Ownership
	Packages  []Package
	// WallHolds says, by domain, that the wall test the ruler names is in
	// the code.
	WallHolds map[string]bool
	// Contracts is, by domain, what other domains use of its Go packages
	// (Surface.Uses). Nil when the tree was not type-checked: the contracts
	// item is then unmeasured for every domain.
	Contracts map[string]ContractUse
	// Routes is, by domain, the routes its api/internal/server/mount_<domain>.go
	// registers; a domain with no such file has no entry.
	Routes map[string]int
}

type Item struct {
	ID       string  `json:"id"`
	Label    string  `json:"label"`
	Max      float64 `json:"max"`
	By       string  `json:"by"`
	Measured bool    `json:"measured"`
	// MeasuredMax is how many of Max points Score is out of: an item may be
	// measured in part (ruler rule 4: the rest is neither zero nor full).
	MeasuredMax float64            `json:"measured_max,omitempty"`
	Score       float64            `json:"score"`
	Measures    map[string]float64 `json:"measures,omitempty"`
	// Evidence is what the score was taken off for, one line each.
	Evidence []string `json:"evidence,omitempty"`
}

type Domain struct {
	Domain   string `json:"domain"`
	Packages int    `json:"packages"`
	Lines    int    `json:"lines"`
	// Score and Max add up the measured machine items only, and of an item
	// measured in part only the part that is (its measured_max).
	Score float64 `json:"score"`
	Max   float64 `json:"max"`
	Items []Item  `json:"items"`
}

type Answer struct {
	RulerVersion int      `json:"ruler_version"`
	Commit       string   `json:"commit"`
	Domains      []Domain `json:"domains"`
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// Graph is the domain of every package of a module and the imports between
// its domains: the one reading of "which domain is this package in" and
// "does this import point the wrong way" (ruler v2). Score and the CI page's
// dependency view (cidomains) both stand on it, so the page cannot draw an
// edge red that the score does not count, or the other way round.
type Graph struct {
	pkgs map[string]graphPackage
	next map[string]map[string]bool
	base string
	back map[[2]string]bool
	// Order is the domains the ownership table declares, in its order, then
	// the domains only a row names, sorted. Mixed is not among them.
	Order []string
}

type graphPackage struct {
	Package
	domain   string
	assigned bool
}

// NewGraph reads the packages against the ownership table. The assembly
// roots (api/cmd/) are left out: a command imports every domain it wires.
func NewGraph(own Ownership, packages []Package) *Graph {
	g := &Graph{pkgs: map[string]graphPackage{}, next: map[string]map[string]bool{}, back: map[[2]string]bool{}}
	for _, p := range packages {
		if strings.HasPrefix(p.Path, assemblyRoots) {
			continue
		}
		d, assigned := own.DomainOf(p.Path)
		g.pkgs[p.Path] = graphPackage{p, d, assigned}
	}
	// The domain graph: a reaches b when a package of a imports one of b.
	for _, p := range g.pkgs {
		for _, dep := range p.Imports {
			if q, ok := g.pkgs[dep]; ok && q.domain != p.domain {
				if g.next[p.domain] == nil {
					g.next[p.domain] = map[string]bool{}
				}
				g.next[p.domain][q.domain] = true
			}
		}
	}
	// The base is the default domain: every domain stands on it.
	if len(own.Default.Domains) == 1 {
		g.base = own.Default.Domains[0]
	}
	seen := map[string]bool{}
	for _, d := range own.Domains {
		g.Order, seen[d.ID] = append(g.Order, d.ID), true
	}
	// A domain the table does not declare but a row names is scored too. Mixed
	// is not a domain: nobody owns its number.
	var extra []string
	for _, p := range g.pkgs {
		if !seen[p.domain] && p.domain != Mixed {
			extra, seen[p.domain] = append(extra, p.domain), true
		}
	}
	sort.Strings(extra)
	g.Order = append(g.Order, extra...)
	return g
}

// Packages are the paths of the packages in the graph, sorted.
// Base is the domain every other stands on, "" when the table names none.
func (g *Graph) Base() string { return g.base }

func (g *Graph) Packages() []string {
	out := make([]string, 0, len(g.pkgs))
	for p := range g.pkgs {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// DomainOf is the domain of a package in the graph (Mixed for a package of
// several); ok is false for a path that is not one of its packages.
func (g *Graph) DomainOf(pkg string) (domain string, ok bool) {
	p, ok := g.pkgs[pkg]
	return p.domain, ok
}

// Imports are the packages of the graph that pkg's production code imports.
func (g *Graph) Imports(pkg string) []string {
	var out []string
	for _, dep := range g.pkgs[pkg].Imports {
		if _, ok := g.pkgs[dep]; ok {
			out = append(out, dep)
		}
	}
	return out
}

// reaches does not pass through the base or through Mixed. Every domain
// imports the base and the shared packages import every domain, so a path
// through either joins any two domains: version 1 counted those and
// called plan -> lineage, lineage -> verification and owner ->
// coordination cycles when none of the three is imported back
// (2026-11-16, found drawing the domain graph from the same imports).
func (g *Graph) reaches(from, to string) bool {
	seen, queue := map[string]bool{from: true}, []string{from}
	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]
		for n := range g.next[at] {
			if n == to {
				return true
			}
			if !seen[n] && n != g.base && n != Mixed {
				seen[n] = true
				queue = append(queue, n)
			}
		}
	}
	return false
}

// WrongWay says an import from a package of domain from to one of domain to
// points the wrong way. An import of the base points the right way whatever
// the base imports in turn, and the shared packages of several domains
// (Mixed) are the base's to answer for, not their importer's. Between two
// other domains neither is below the other: a mutual dependency counts
// against both.
func (g *Graph) WrongWay(from, to string) bool {
	if to == g.base || (to == Mixed && from != g.base) {
		return false
	}
	k := [2]string{to, from}
	if v, ok := g.back[k]; ok {
		return v
	}
	g.back[k] = g.reaches(to, from)
	return g.back[k]
}

// Score computes every domain's score. Domains are those the ownership
// table declares, in its order.
func Score(in Input) Answer {
	graph := NewGraph(in.Ownership, in.Packages)
	pkgs, wrongWay, order, base := graph.pkgs, graph.WrongWay, graph.Order, graph.base

	out := Answer{RulerVersion: in.Ruler.Version, Commit: in.Commit, Domains: []Domain{}}
	for _, d := range order {
		row := Domain{Domain: d}
		var edges, cross []string
		assignedLines := 0
		users := map[string]bool{}
		for _, p := range pkgs {
			if p.domain == d {
				continue
			}
			for _, dep := range p.Imports {
				if q, ok := pkgs[dep]; ok && q.domain == d {
					users[p.Path] = true
				}
			}
		}
		for _, p := range pkgs {
			if p.domain != d {
				continue
			}
			row.Packages++
			row.Lines += p.Lines
			if p.assigned {
				assignedLines += p.Lines
			}
			for _, dep := range p.Imports {
				if q, ok := pkgs[dep]; ok && q.domain != d && wrongWay(d, q.domain) {
					edges = append(edges, p.Path+" -> "+dep+" ("+q.domain+")")
				} else if ok && d != base && q.domain != d && q.domain != base {
					// Another domain's package, or one several domains share:
					// no cycle, and still two domains that change together.
					cross = append(cross, p.Path+" -> "+dep+" ("+q.domain+")")
				}
			}
		}
		sort.Strings(edges)
		sort.Strings(cross)
		for _, it := range in.Ruler.Items {
			item := Item{ID: it.ID, Label: it.Label, Max: it.Max, By: it.By}
			if it.ID == "dependency_direction" && row.Packages > 0 {
				item.Measured = true
				item.MeasuredMax = it.Max
				direction := 17.0
				if n := len(edges) + len(cross); n > 0 {
					direction = math.Max(11, 17-math.Ceil(float64(n)/2))
				}
				wall, share := 0.0, 0.0
				if in.WallHolds[d] {
					wall = 1
				}
				if row.Lines > 0 {
					share = float64(assignedLines) / float64(row.Lines)
				}
				item.Score = round1(direction + 4*wall + 4*share)
				item.Measures = map[string]float64{"cycle_edges": float64(len(edges)), "cross_edges": float64(len(cross)), "wall": wall, "assigned_share": math.Round(share*100) / 100}
				item.Evidence = append(append([]string{}, edges...), cross...)
				if wall == 0 {
					item.Evidence = append(item.Evidence, "no wall: nothing stops a new import that points the wrong way")
				}
				if share < 1 {
					item.Evidence = append(item.Evidence, fmt.Sprintf("%d of %d production lines are in packages whose row is not 'assigned'", row.Lines-assignedLines, row.Lines))
				}
			}
			if it.ID == "contracts" && it.Measured && row.Packages > 0 && in.Contracts != nil {
				contracts(&item, in.Contracts[d], len(users), in.Routes, d)
			}
			if item.Measured {
				row.Score += item.Score
				row.Max += item.MeasuredMax
			}
			row.Items = append(row.Items, item)
		}
		row.Score = round1(row.Score)
		out.Domains = append(out.Domains, row)
	}
	return out
}
