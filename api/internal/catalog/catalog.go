// Package catalog is the map's pool of what it cannot perceive (0.2 map line,
// Owner 2026-11-20): a repository's files that no row of its ownership table
// covers. The map cannot see them, which does not mean they are not there;
// each is a defect to review: claim it into the map, delete it, or mark it
// unknown. The pool is a reading the machine produces on every scan; nothing
// in it is typed by hand, and nothing is added to the map by the scan.
//
// It decides nothing and gates nothing. In particular the ownership table's
// default (a package with no row belongs to foundation) still holds for the
// domain walls; here such a package is in the pool, because no row names it
// (coordinator ruling 2026-11-20: changing the default is a referee rule).
package catalog

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
)

// Version is the version of the pool's body.
const Version = 1

// The files the scan reads at a commit.
const (
	OwnershipPath = "config/domain-ownership.json"
	ConsolePath   = "console/domain-ownership.json"
	// ReviewPath holds the review of pool entries that stay in the pool: the
	// ones to delete and the ones that cannot be measured. A claimed entry
	// leaves the pool by becoming a row, so it needs no review line.
	ReviewPath = "config/catalog-review.json"
)

// The verdicts a review may give an entry that stays in the pool.
const (
	VerdictDelete  = "delete"
	VerdictUnknown = "unknown"
)

// Table is what the scan reads of the ownership tables: the package rows, the
// front end's groups, and the directories its Go code sits under.
type Table struct {
	Packages map[string]bool // directory -> the table has a row for it
	Groups   []string        // front-end group prefixes, longest first
	GoRoots  []string        // top directories of the package rows
	Console  string          // top directory of the front end, "" for none
	// Domains are the domains each package row and each group names.
	Domains map[string][]string
}

type row struct {
	Domains []string `json:"domains"`
}

// ParseTable reads the ownership table and, when present, the console's.
func ParseTable(own, console []byte) (Table, error) {
	var o struct {
		Packages map[string]row `json:"packages"`
	}
	if err := json.Unmarshal(own, &o); err != nil {
		return Table{}, fmt.Errorf("%s does not parse: %w", OwnershipPath, err)
	}
	t := Table{Packages: map[string]bool{}, Domains: map[string][]string{}}
	roots := map[string]bool{}
	for p, r := range o.Packages {
		t.Packages[p] = true
		t.Domains[p] = r.Domains
		if r, _, _ := strings.Cut(p, "/"); r != "" {
			roots[r] = true
		}
	}
	for r := range roots {
		t.GoRoots = append(t.GoRoots, r)
	}
	sort.Strings(t.GoRoots)
	if len(console) > 0 {
		var c struct {
			Frontend struct {
				Root   string         `json:"root"`
				Groups map[string]row `json:"groups"`
			} `json:"frontend"`
		}
		if err := json.Unmarshal(console, &c); err != nil {
			return Table{}, fmt.Errorf("%s does not parse: %w", ConsolePath, err)
		}
		t.Console, _, _ = strings.Cut(strings.Trim(c.Frontend.Root, "/"), "/")
		for g, r := range c.Frontend.Groups {
			g = strings.Trim(g, "/")
			t.Groups = append(t.Groups, g)
			t.Domains[g] = r.Domains
		}
		sort.Slice(t.Groups, func(i, j int) bool {
			if len(t.Groups[i]) != len(t.Groups[j]) {
				return len(t.Groups[i]) > len(t.Groups[j])
			}
			return t.Groups[i] < t.Groups[j]
		})
	}
	return t, nil
}

// Review is one line of ReviewPath.
type Review struct {
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
	By      string `json:"by"`
	On      string `json:"on"`
}

// ParseReviews reads ReviewPath; an empty file is no review.
func ParseReviews(raw []byte) (map[string]Review, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return map[string]Review{}, nil
	}
	var r struct {
		Reviews map[string]Review `json:"reviews"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("%s does not parse: %w", ReviewPath, err)
	}
	for p, v := range r.Reviews {
		if v.Verdict != VerdictDelete && v.Verdict != VerdictUnknown {
			return nil, fmt.Errorf("%s: %s has verdict %q; only %q and %q stay in the pool (a claimed entry becomes a row)", ReviewPath, p, v.Verdict, VerdictDelete, VerdictUnknown)
		}
	}
	if r.Reviews == nil {
		r.Reviews = map[string]Review{}
	}
	return r.Reviews, nil
}

// Entry is one thing the map cannot perceive: a directory (or, for files at
// the repository's root, the root) whose files no row covers.
type Entry struct {
	Path string `json:"path"`
	// Kind is "go-package" for a Go package with no row, "console" for front
	// end files in no group, otherwise the top directory ("mcp", "agent", …),
	// "root" for files at the repository's root, "repository" for a
	// repository with no ownership table.
	Kind  string `json:"kind"`
	Files int    `json:"files"`
	// FirstSeen is when a scan first found it in the pool (kept across scans).
	FirstSeen time.Time `json:"first_seen"`
	// Review is absent while nobody has reviewed it.
	Review *Review `json:"review,omitempty"`
}

// Scan is the pool of one commit: files are every file path of the tree,
// isPackage says a directory holds Go files directly.
func Scan(t Table, files []string, isPackage func(dir string) bool) []Entry {
	count := map[string]int{}
	kind := map[string]string{}
	for _, f := range files {
		f = strings.Trim(path.Clean(f), "/")
		if f == "" || f == "." {
			continue
		}
		p, k, ok := t.place(f, isPackage)
		if ok {
			continue
		}
		count[p]++
		kind[p] = k
	}
	out := make([]Entry, 0, len(count))
	for p, n := range count {
		out = append(out, Entry{Path: p, Kind: kind[p], Files: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// place is the pool entry a file falls in (ok true: the map covers it).
func (t Table) place(f string, isPackage func(string) bool) (entry, kind string, covered bool) {
	if !strings.Contains(f, "/") {
		return ".", "root", false
	}
	top, _, _ := strings.Cut(f, "/")
	if t.Console != "" && top == t.Console {
		for _, g := range t.Groups {
			if f == g || strings.HasPrefix(f, g+"/") {
				return "", "", true
			}
		}
		return twoLevels(f), "console", false
	}
	for _, r := range t.GoRoots {
		if r != top {
			continue
		}
		dir := path.Dir(f)
		for strings.Contains(dir, "/") && !t.Packages[dir] && !(isPackage != nil && isPackage(dir)) {
			dir = path.Dir(dir)
		}
		if t.Packages[dir] {
			return "", "", true
		}
		if isPackage != nil && isPackage(dir) {
			return dir, "go-package", false
		}
		return dir, top, false
	}
	return twoLevels(f), top, false
}

// twoLevels is the directory of a file cut to two levels ("mcp/platform"
// for mcp/platform/src/x.ts, "config" for config/x.yaml).
func twoLevels(f string) string {
	dir := path.Dir(f)
	parts := strings.SplitN(dir, "/", 3)
	if len(parts) > 2 {
		parts = parts[:2]
	}
	return strings.Join(parts, "/")
}

// Pool is the answer of GET /api/v1/catalog/unperceived.
type Pool struct {
	Version int    `json:"version"`
	Repo    string `json:"repo"`
	Commit  string `json:"commit"`
	// Measured is false when the scan could not read the repository; Note
	// says why. An unmeasured pool is not an empty one.
	Measured  bool      `json:"measured"`
	Note      string    `json:"note"`
	ScannedAt time.Time `json:"scanned_at"`
	Entries   []Entry   `json:"entries"`
	// Count is len(Entries); Files the files in them; Unreviewed the entries
	// with no review; OldestDays the age of the oldest entry by FirstSeen
	// (absent with no entry).
	Count      int  `json:"count"`
	Files      int  `json:"files"`
	Unreviewed int  `json:"unreviewed"`
	OldestDays *int `json:"oldest_days,omitempty"`
}

// Summarize fills the counts of p at now: first seen from before (the last
// pool kept, nil for none), the reviews from reviews.
func (p *Pool) Summarize(now time.Time, before *Pool, reviews map[string]Review) {
	seen := map[string]time.Time{}
	if before != nil {
		for _, e := range before.Entries {
			seen[e.Path] = e.FirstSeen
		}
	}
	p.Count, p.Files, p.Unreviewed, p.OldestDays = len(p.Entries), 0, 0, nil
	var oldest time.Time
	for i := range p.Entries {
		e := &p.Entries[i]
		if t, ok := seen[e.Path]; ok && !t.IsZero() {
			e.FirstSeen = t
		} else if e.FirstSeen.IsZero() {
			e.FirstSeen = now
		}
		if r, ok := reviews[e.Path]; ok {
			r := r
			e.Review = &r
		} else {
			e.Review = nil
			p.Unreviewed++
		}
		p.Files += e.Files
		if oldest.IsZero() || e.FirstSeen.Before(oldest) {
			oldest = e.FirstSeen
		}
	}
	if !oldest.IsZero() {
		d := int(now.Sub(oldest).Hours() / 24)
		p.OldestDays = &d
	}
}
