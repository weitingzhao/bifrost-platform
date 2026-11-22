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

// The verdicts a review may give an entry. A claim names the domain it
// proposes and stays a review until that domain's row is in the table (the
// domain fills its own row; the map line does not).
const (
	VerdictClaim   = "claim"
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
	// Paths are the table's path rows (paths): directories or files outside
	// the Go packages and the front end's groups, longest first; "." is the
	// files at the repository's root.
	Paths []string
	// Domains are the domains each package row, group and path row names.
	Domains map[string][]string
}

type row struct {
	Domains []string `json:"domains"`
}

// ParseTable reads the ownership table and, when present, the console's.
func ParseTable(own, console []byte) (Table, error) {
	var o struct {
		Packages map[string]row `json:"packages"`
		Paths    map[string]row `json:"paths"`
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
	for p, r := range o.Paths {
		p = strings.Trim(p, "/")
		if p == "" {
			p = "."
		}
		t.Paths = append(t.Paths, p)
		t.Domains[p] = r.Domains
	}
	sortLongestFirst(t.Paths)
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
		sortLongestFirst(t.Groups)
	}
	return t, nil
}

func sortLongestFirst(s []string) {
	sort.Slice(s, func(i, j int) bool {
		if len(s[i]) != len(s[j]) {
			return len(s[i]) > len(s[j])
		}
		return s[i] < s[j]
	})
}

// PathRow is the path row that covers f, "" for none.
func PathRow(paths []string, f string) string {
	for _, p := range paths {
		if (p == "." && !strings.Contains(f, "/")) || f == p || strings.HasPrefix(f, p+"/") {
			return p
		}
	}
	return ""
}

// Review is one line of ReviewPath.
type Review struct {
	Verdict string `json:"verdict"`
	// Domain is the domain a claim proposes; the entry leaves the pool when
	// that domain's row is in the table.
	Domain string `json:"domain,omitempty"`
	Reason string `json:"reason"`
	By     string `json:"by"`
	On     string `json:"on"`
}

// ParseReviews reads ReviewPath; an empty file is no review.
func ParseReviews(raw []byte) (map[string]Review, error) {
	own, _, err := ParseReviewFile(raw)
	return own, err
}

// ParseReviewFile reads ReviewPath: the reviews of this repository's entries,
// and under "repositories" the reviews of other repositories' entries
// (repository name, then entry path). A repository with no table has no
// review file of its own; the map reviews it from where the map lives.
func ParseReviewFile(raw []byte) (own map[string]Review, others map[string]map[string]Review, err error) {
	own, others = map[string]Review{}, map[string]map[string]Review{}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return own, others, nil
	}
	var r struct {
		Reviews      map[string]Review            `json:"reviews"`
		Repositories map[string]map[string]Review `json:"repositories"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, nil, fmt.Errorf("%s does not parse: %w", ReviewPath, err)
	}
	if err := checkReviews("", r.Reviews); err != nil {
		return nil, nil, err
	}
	for repo, rs := range r.Repositories {
		if !repoName.MatchString(repo) {
			return nil, nil, fmt.Errorf("%s: %q is not a repository name", ReviewPath, repo)
		}
		if err := checkReviews(repo+":", rs); err != nil {
			return nil, nil, err
		}
		others[repo] = rs
	}
	for p, v := range r.Reviews {
		own[p] = v
	}
	return own, others, nil
}

func checkReviews(prefix string, rs map[string]Review) error {
	for p, v := range rs {
		switch {
		case v.Verdict == VerdictClaim && v.Domain == "":
			return fmt.Errorf("%s: %s%s is claimed for no domain", ReviewPath, prefix, p)
		case v.Verdict != VerdictClaim && v.Verdict != VerdictDelete && v.Verdict != VerdictUnknown:
			return fmt.Errorf("%s: %s%s has verdict %q; a verdict is %q (with a domain), %q or %q", ReviewPath, prefix, p, v.Verdict, VerdictClaim, VerdictDelete, VerdictUnknown)
		case strings.TrimSpace(v.Reason) == "":
			return fmt.Errorf("%s: %s%s has no reason", ReviewPath, prefix, p)
		}
	}
	return nil
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
// A Go package is covered only by its package row: a path row never covers
// a package (the domain walls read package rows, and the map must not say
// more than they do).
func (t Table) place(f string, isPackage func(string) bool) (entry, kind string, covered bool) {
	byPath := PathRow(t.Paths, f) != ""
	if !strings.Contains(f, "/") {
		if byPath {
			return "", "", true
		}
		return ".", "root", false
	}
	top, _, _ := strings.Cut(f, "/")
	if t.Console != "" && top == t.Console {
		for _, g := range t.Groups {
			if f == g || strings.HasPrefix(f, g+"/") {
				return "", "", true
			}
		}
		if byPath {
			return "", "", true
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
		if byPath {
			return "", "", true
		}
		return dir, top, false
	}
	if byPath {
		return "", "", true
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
	// The equation (2026-11-20, perceive first: "everything is on the map or
	// in the pool, no third place"). Total is every file of the tree (every
	// workload of the cluster); OnMap those a row covers, counted with
	// OwnerOf, a reader other than the scan's own placing; OffMap the files
	// in the entries. Gap is Total - OnMap - OffMap: not 0 means the scan
	// misses or double-counts something, and the fresh lights go bad.
	Total  int `json:"total"`
	OnMap  int `json:"on_map"`
	OffMap int `json:"off_map"`
	Gap    int `json:"gap"`
	// Whole is true for a repository kept as one entry on purpose: a
	// tenant's, whose inside the map does not look at this round (Owner
	// 2026-11-20: the platform's own first).
	Whole bool `json:"whole,omitempty"`
	// Covered lists, for the cluster and the platform database only, every
	// workload or table on the map and the row covering it; ByNamespace marks
	// one covered only by a whole namespace's (schema's) row (2026-11-20: 90
	// of 107 workloads were).
	Covered []Covered `json:"covered,omitempty"`
	// StaleRows are the table's rows that cover nothing in the tree: a
	// package row whose directory has no file, a group or path row no file
	// sits under, a workload or table row with nothing in it (2026-11-21:
	// what was removed but is still registered; the map saw what was added
	// and not what was taken away). Empty for a repository with no table.
	StaleRows []string `json:"stale_rows"`
}

// staleRows is every row of t that covers no file.
func staleRows(t Table, files []string) []string {
	dirs, prefixes := map[string]bool{}, map[string]bool{}
	for _, f := range files {
		f = strings.Trim(path.Clean(f), "/")
		dirs[path.Dir(f)] = true
		for d := f; ; {
			prefixes[d] = true
			i := strings.LastIndex(d, "/")
			if i < 0 {
				break
			}
			d = d[:i]
		}
	}
	rootFile := false
	for _, f := range files {
		if !strings.Contains(strings.Trim(f, "/"), "/") {
			rootFile = true
			break
		}
	}
	out := []string{}
	for p := range t.Packages {
		if !dirs[p] {
			out = append(out, p)
		}
	}
	for _, rows := range [][]string{t.Groups, t.Paths} {
		for _, r := range rows {
			if (r == "." && !rootFile) || (r != "." && !prefixes[r]) {
				out = append(out, r)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Covered is one workload on the map.
type Covered struct {
	Path        string   `json:"path"`
	Kind        string   `json:"kind"`
	Domains     []string `json:"domains"`
	By          string   `json:"by"`
	ByNamespace bool     `json:"by_namespace,omitempty"`
}

// equation fills Total, OnMap, OffMap and Gap (and Covered for the cluster)
// from files and the owners o (nil: nothing is on the map).
func (p *Pool) equation(files []string, o *Owners) {
	p.Total, p.OnMap, p.OffMap, p.Covered = 0, 0, 0, nil
	pool := &Pool{Entries: p.Entries}
	for _, f := range files {
		f = strings.Trim(path.Clean(f), "/")
		if f == "" || f == "." {
			continue
		}
		p.Total++
		if o == nil {
			continue
		}
		w := OwnerOf(o, pool, f)
		if !w.Perceived {
			continue
		}
		p.OnMap++
		if p.Repo == ClusterRepo || p.Repo == DatabaseRepo {
			row := strings.TrimPrefix(strings.TrimPrefix(w.By, "path "), "package ")
			p.Covered = append(p.Covered, Covered{Path: path.Dir(f), Kind: path.Base(f), Domains: w.Domains,
				By: w.By, ByNamespace: !strings.Contains(row, "/")})
		}
	}
	for _, e := range p.Entries {
		p.OffMap += e.Files
	}
	p.Gap = p.Total - p.OnMap - p.OffMap
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
