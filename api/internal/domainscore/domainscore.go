// Package domainscore scores each domain's code against config/quality-ruler.json:
// one ruler for every domain, a version on every score, so that a domain's
// number can be compared with its own earlier numbers (Owner 2026-11-16).
//
// It is a reading: it decides nothing and gates nothing. Version 1 measures
// one item, dependency direction; the others are reported unmeasured.
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
const RulerVersion = 2

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
	Items   []struct {
		ID       string
		Label    string
		Max      float64
		By       string
		Measured bool
	}
	Walls map[string]struct{ File, Test string }
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
	for _, it := range r.Items {
		total += it.Max
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
}

type Item struct {
	ID       string             `json:"id"`
	Label    string             `json:"label"`
	Max      float64            `json:"max"`
	By       string             `json:"by"`
	Measured bool               `json:"measured"`
	Score    float64            `json:"score"`
	Measures map[string]float64 `json:"measures,omitempty"`
	// Evidence is what the score was taken off for, one line each.
	Evidence []string `json:"evidence,omitempty"`
}

type Domain struct {
	Domain   string `json:"domain"`
	Packages int    `json:"packages"`
	Lines    int    `json:"lines"`
	// Score and Max add up the measured machine items only.
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

// Score computes every domain's score. Domains are those the ownership
// table declares, in its order, then Mixed when a package is.
func Score(in Input) Answer {
	type pkg struct {
		Package
		domain   string
		assigned bool
	}
	pkgs := map[string]pkg{}
	for _, p := range in.Packages {
		if strings.HasPrefix(p.Path, assemblyRoots) {
			continue
		}
		d, assigned := in.Ownership.DomainOf(p.Path)
		pkgs[p.Path] = pkg{p, d, assigned}
	}
	// The domain graph: a reaches b when a package of a imports one of b.
	next := map[string]map[string]bool{}
	for _, p := range pkgs {
		for _, dep := range p.Imports {
			if q, ok := pkgs[dep]; ok && q.domain != p.domain {
				if next[p.domain] == nil {
					next[p.domain] = map[string]bool{}
				}
				next[p.domain][q.domain] = true
			}
		}
	}
	// The base is the default domain: every domain stands on it.
	base := ""
	if len(in.Ownership.Default.Domains) == 1 {
		base = in.Ownership.Default.Domains[0]
	}
	// reaches does not pass through the base or through Mixed. Every domain
	// imports the base and the shared packages import every domain, so a path
	// through either joins any two domains: version 1 counted those and
	// called plan -> lineage, lineage -> verification and owner ->
	// coordination cycles when none of the three is imported back
	// (2026-11-16, found drawing the domain graph from the same imports).
	reaches := func(from, to string) bool {
		seen, queue := map[string]bool{from: true}, []string{from}
		for len(queue) > 0 {
			at := queue[0]
			queue = queue[1:]
			for n := range next[at] {
				if n == to {
					return true
				}
				if !seen[n] && n != base && n != Mixed {
					seen[n] = true
					queue = append(queue, n)
				}
			}
		}
		return false
	}
	back := map[[2]string]bool{}
	comesBack := func(from, to string) bool {
		k := [2]string{to, from}
		if v, ok := back[k]; ok {
			return v
		}
		back[k] = reaches(to, from)
		return back[k]
	}

	// An import of the base points the right way whatever the base imports in
	// turn, and the shared packages of several domains (Mixed) are the base's
	// to answer for, not their importer's. Between two other domains neither
	// is below the other: a mutual dependency counts against both.
	wrongWay := func(from, to string) bool {
		if to == base || (to == Mixed && from != base) {
			return false
		}
		return comesBack(from, to)
	}

	var order []string
	seen := map[string]bool{}
	for _, d := range in.Ownership.Domains {
		order, seen[d.ID] = append(order, d.ID), true
	}
	// A domain the table does not declare but a row names is scored too. Mixed
	// is not a domain: nobody owns its number.
	var extra []string
	for _, p := range pkgs {
		if !seen[p.domain] && p.domain != Mixed {
			extra, seen[p.domain] = append(extra, p.domain), true
		}
	}
	sort.Strings(extra)
	order = append(order, extra...)

	out := Answer{RulerVersion: in.Ruler.Version, Commit: in.Commit, Domains: []Domain{}}
	for _, d := range order {
		row := Domain{Domain: d}
		var edges []string
		assignedLines := 0
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
				}
			}
		}
		sort.Strings(edges)
		for _, it := range in.Ruler.Items {
			item := Item{ID: it.ID, Label: it.Label, Max: it.Max, By: it.By}
			if it.ID == "dependency_direction" && row.Packages > 0 {
				item.Measured = true
				direction := 17.0
				if n := len(edges); n > 0 {
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
				item.Measures = map[string]float64{"cycle_edges": float64(len(edges)), "wall": wall, "assigned_share": math.Round(share*100) / 100}
				item.Evidence = edges
				if wall == 0 {
					item.Evidence = append(item.Evidence, "no wall: nothing stops a new import that points the wrong way")
				}
				if share < 1 {
					item.Evidence = append(item.Evidence, fmt.Sprintf("%d of %d production lines are in packages whose row is not 'assigned'", row.Lines-assignedLines, row.Lines))
				}
				row.Score += item.Score
				row.Max += item.Max
			}
			row.Items = append(row.Items, item)
		}
		row.Score = round1(row.Score)
		out.Domains = append(out.Domains, row)
	}
	return out
}
