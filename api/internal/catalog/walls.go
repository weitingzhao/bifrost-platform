package catalog

import (
	"encoding/json"
	"log/slog"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Walls and policies (2026-11-26, the legislation line: the machine walls and
// the standards written into code were found by agents sent to look, right
// once and drifting after; the system counts them instead; LAW-1 five and
// nine, LAW-8 two; convention: shared notes domains/1-plan/round6/
// wall-tag-convention.md). Each wall declares itself beside its code:
//
//	wall: <id> law: <LAW-k item> guards: <what> revoke: <when>
//	wall: <id> dup-of: <main id>          (one more place guarding the same)
//	policy: <id> law: <LAW-k item> ...    (a hard-coded standard that judges)
//
// The scan reads the directories the index names for class wall (and the
// same for policy) and finds the enforcement points by structure: a deny
// kind of preflight.js, a rule of an auto-mode file's hard_deny, soft_deny or
// ask, a Test of codeguard, a C, D or X action of the action catalog, a
// refusal of the release policy, a check step of a Tekton task. A point with
// a declaration in the few lines above it (or, for an auto-mode rule, inside
// its text) is anchored when its law is in force, stale when not; without
// one it is an orphan; a dup-of naming no main declaration is broken.
// Policies have no structure to find: the declarations are what is counted.
// Measures only; it refuses nothing.

const keptWallsPrefix = "walls-"

// WallsRule is the version of how points and declarations are read: a kept
// reading of another rule is read again even when the commit is the same
// (11-26: the first rule's false declarations stayed on an unchanged infra).
const WallsRule = 2

// WallLookback is how many lines above a point a declaration may stand.
const WallLookback = 8

var (
	// A declaration opens a comment line (or, in an auto-mode rule, stands in
	// its text) and its id has a dot ("preflight.shared-worktree-add"): STG
	// 11-26 02:16Z counted 38 "policies" and dozens of walls out of prose
	// ("policy: delivery") and old references ("wall: 106").
	declRe       = regexp.MustCompile(`(?:^\s*(?://+|#+|/\*+|\*+|--)\s*|^\s*|\s)(wall|policy):\s*([A-Za-z0-9_-]+\.[A-Za-z0-9_.:/-]*[A-Za-z0-9_-])`)
	commentRe    = regexp.MustCompile(`^\s*(?://|#|/\*|\*|--)`)
	declLawRe    = regexp.MustCompile(`\blaw:\s*((?:LAW|CORE)-\d+)`)
	declDupRe    = regexp.MustCompile(`\bdup-of:\s*([A-Za-z0-9_.:/-]+)`)
	preflightRe  = regexp.MustCompile(`deny:\s*true,\s*kind:\s*'([^']+)'`)
	testFuncRe   = regexp.MustCompile(`^func (Test\w+)\(`)
	actionTierRe = regexp.MustCompile(`ID:\s*"([a-z0-9_]+)",\s*Tier:\s*Tier([CDX])\b`)
	refusedRe    = regexp.MustCompile(`\brefused\(`)
	stepNameRe   = regexp.MustCompile(`^(\s*)- name:\s*["']?([A-Za-z0-9_.-]+)`)
	checkWordRe  = regexp.MustCompile(`check|guard|gate|freeze|verify`)
)

// WallDecl is one declaration as written.
type WallDecl struct {
	Kind  string `json:"kind"` // wall or policy
	ID    string `json:"id"`
	Law   string `json:"law,omitempty"`
	DupOf string `json:"dup_of,omitempty"`
	File  string `json:"file"`
	Line  int    `json:"line"`
}

// WallPoint is one enforcement point found by structure.
type WallPoint struct {
	Kind   string `json:"kind"` // preflight, auto-mode, codeguard, action, releasepolicy, tekton
	Name   string `json:"name"`
	File   string `json:"file"`
	Line   int    `json:"line"`
	Decl   string `json:"decl,omitempty"` // the id of the declaration beside it
	Domain string `json:"domain,omitempty"`
}

type keptWalls struct {
	Rule   int         `json:"rule"`
	Repo   string      `json:"repo"`
	Commit string      `json:"commit"`
	At     time.Time   `json:"at"`
	Points []WallPoint `json:"points"`
	Decls  []WallDecl  `json:"decls"`
	// DeclDomains is each declaration's file's domain, by "file:line".
	DeclDomains map[string]string `json:"decl_domains"`
}

// classSources are a class's sources in the kept index, "<repo>:<path>".
func (s *Scanner) classSources(class string) []string {
	raw, err := s.kept().Read(keptIndex)
	if err != nil || raw == nil {
		return nil
	}
	var ix Index
	if json.Unmarshal(raw, &ix) != nil {
		return nil
	}
	for _, c := range ix.Classes {
		if c.Class == class {
			return c.Sources
		}
	}
	return nil
}

// wallRoots are the directories of repo the index names for walls and
// policies.
func (s *Scanner) wallRoots(repo string) []string {
	seen := map[string]bool{}
	var roots []string
	for _, class := range []string{"wall", "policy"} {
		for _, src := range s.classSources(class) {
			r, dir, ok := strings.Cut(src, ":")
			if ok && r == repo && !seen[dir] {
				seen[dir] = true
				roots = append(roots, strings.TrimSuffix(dir, "/"))
			}
		}
	}
	return roots
}

func wallFile(f string) bool {
	if strings.Contains(f, "node_modules/") || strings.Contains(f, "/testdata/") || strings.Contains(f, ".generated.") ||
		strings.Contains(f, "/fixtures/") || strings.Contains(f, "__tests__/") {
		return false
	}
	switch path.Ext(f) {
	case ".go", ".js", ".mjs", ".ts", ".tsx", ".py", ".sh", ".yaml", ".yml", ".json":
		return true
	}
	return false
}

// declsIn reads the declarations of one file's comment lines (rule says
// the lines are rule texts, read whole).
func declsIn(file string, lines []string) []WallDecl { return readDecls(file, lines, false) }

func readDecls(file string, lines []string, rule bool) []WallDecl {
	var out []WallDecl
	for i, l := range lines {
		if !rule && !commentRe.MatchString(l) {
			continue
		}
		for _, m := range declRe.FindAllStringSubmatchIndex(l, -1) {
			d := WallDecl{Kind: l[m[2]:m[3]], ID: strings.TrimRight(l[m[4]:m[5]], ".,;"), File: file, Line: i + 1}
			rest := l[m[1]:]
			if next := declRe.FindStringIndex(rest); next != nil {
				rest = rest[:next[0]]
			}
			if lm := declLawRe.FindStringSubmatch(rest); lm != nil {
				d.Law = lm[1]
			}
			if dm := declDupRe.FindStringSubmatch(rest); dm != nil {
				d.DupOf = strings.TrimRight(dm[1], ".,;")
			}
			out = append(out, d)
		}
	}
	return out
}

// pointsIn finds one file's enforcement points by structure.
func pointsIn(file string, raw []byte, lines []string) []WallPoint {
	var out []WallPoint
	base := path.Base(file)
	switch {
	case base == "preflight.js":
		seen := map[string]bool{}
		for i, l := range lines {
			if m := preflightRe.FindStringSubmatch(l); m != nil && !seen[m[1]] {
				seen[m[1]] = true
				out = append(out, WallPoint{Kind: "preflight", Name: m[1], File: file, Line: i + 1})
			}
		}
	case strings.HasSuffix(base, ".autoMode.json"):
		var doc map[string]any
		if json.Unmarshal(raw, &doc) != nil {
			return nil
		}
		for _, list := range []string{"hard_deny", "soft_deny", "ask"} {
			rules, _ := doc[list].([]any)
			for i, r := range rules {
				text, _ := r.(string)
				if text == "" || strings.HasPrefix(text, "$") {
					continue
				}
				p := WallPoint{Kind: "auto-mode", Name: list + "[" + itoa(i) + "]", File: file}
				if d := readDecls(file, []string{text}, true); len(d) > 0 && d[0].Kind == "wall" {
					p.Decl = d[0].ID
				}
				out = append(out, p)
			}
		}
	case strings.Contains(file, "/codeguard/") && strings.HasSuffix(base, "_test.go"):
		for i, l := range lines {
			if m := testFuncRe.FindStringSubmatch(l); m != nil {
				out = append(out, WallPoint{Kind: "codeguard", Name: m[1], File: file, Line: i + 1})
			}
		}
	case strings.HasSuffix(file, "internal/actions/catalog.go"):
		for i, l := range lines {
			if m := actionTierRe.FindStringSubmatch(l); m != nil {
				out = append(out, WallPoint{Kind: "action", Name: m[1] + " (" + m[2] + ")", File: file, Line: i + 1})
			}
		}
	case strings.Contains(file, "internal/releasepolicy/") && path.Ext(file) == ".go" && !strings.HasSuffix(base, "_test.go"):
		for i, l := range lines {
			t := strings.TrimSpace(l)
			if refusedRe.MatchString(l) && !strings.HasPrefix(t, "//") && !strings.HasPrefix(t, "func refused") {
				out = append(out, WallPoint{Kind: "releasepolicy", Name: base + ":" + itoa(i+1), File: file, Line: i + 1})
			}
		}
	case strings.Contains(file, "k8s/cicd/") && (path.Ext(file) == ".yaml" || path.Ext(file) == ".yml"):
		steps := -1
		for i, l := range lines {
			indent := len(l) - len(strings.TrimLeft(l, " "))
			t := strings.TrimSpace(l)
			if t == "steps:" {
				steps = indent
				continue
			}
			if steps >= 0 && t != "" && indent <= steps && !strings.HasPrefix(t, "-") {
				steps = -1
			}
			if steps < 0 {
				continue
			}
			if m := stepNameRe.FindStringSubmatch(l); m != nil && len(m[1]) <= steps+2 && checkWordRe.MatchString(m[2]) {
				out = append(out, WallPoint{Kind: "tekton", Name: m[2], File: file, Line: i + 1})
			}
		}
	}
	// A declaration in the few lines above a point (or on its line) is its,
	// up to the point above it: one declaration belongs to one point.
	at := map[int]bool{}
	for _, p := range out {
		at[p.Line] = true
	}
	for i := range out {
		if out[i].Line == 0 || out[i].Decl != "" {
			continue
		}
		for j := out[i].Line - 1; j >= 0 && j >= out[i].Line-1-WallLookback; j-- {
			if j < out[i].Line-1 && at[j+1] {
				break
			}
			if d := declsIn(file, []string{lines[j]}); len(d) > 0 && d[0].Kind == "wall" {
				out[i].Decl = d[0].ID
				break
			}
		}
	}
	return out
}

func itoa(n int) string { return strconv.Itoa(n) }

// keepWalls reads the wall roots of repo's tree when its commit changed.
func (s *Scanner) keepWalls(repo string, t Tree, now time.Time) {
	roots := s.wallRoots(repo)
	if len(roots) == 0 {
		return
	}
	if raw, err := s.kept().Read(keptWallsPrefix + repo); err == nil && raw != nil {
		var k keptWalls
		if json.Unmarshal(raw, &k) == nil && k.Commit == t.Commit && k.Rule == WallsRule {
			return
		}
	}
	owners, _ := s.lastOwners(repo)
	pool, _ := s.Last(repo)
	domainOf := func(f string) string {
		o := OwnerOf(owners, pool, f)
		if len(o.Domains) > 0 {
			return o.Domains[0]
		}
		return ""
	}
	k := keptWalls{Rule: WallsRule, Repo: repo, Commit: t.Commit, At: now.UTC(), Points: []WallPoint{}, Decls: []WallDecl{}, DeclDomains: map[string]string{}}
	for _, f := range t.Files {
		under := false
		for _, r := range roots {
			if r == "" || f == r || strings.HasPrefix(f, r+"/") {
				under = true
				break
			}
		}
		if !under || !wallFile(f) {
			continue
		}
		raw, ok, err := t.Read(f)
		if err != nil || !ok || len(raw) > RepoRefMaxBytes {
			continue
		}
		text := string(raw)
		lines := strings.Split(text, "\n")
		var decls []WallDecl
		if strings.Contains(text, "wall:") || strings.Contains(text, "policy:") {
			decls = declsIn(f, lines)
		}
		points := pointsIn(f, raw, lines)
		if len(decls) == 0 && len(points) == 0 {
			continue
		}
		d := domainOf(f)
		for _, x := range decls {
			k.Decls = append(k.Decls, x)
			if d != "" {
				k.DeclDomains[x.File+":"+itoa(x.Line)] = d
			}
		}
		for _, p := range points {
			p.Domain = d
			k.Points = append(k.Points, p)
		}
	}
	if out, err := json.Marshal(k); err == nil {
		if err := s.kept().Write(keptWallsPrefix+repo, out); err != nil {
			slog.Warn("catalog: keep walls", "repo", repo, "err", err)
		}
	}
}

// WallCounts are one kind's (walls or policies) numbers.
type WallCounts struct {
	Total    int `json:"total"`
	Anchored int `json:"anchored"`
	Orphan   int `json:"orphan"`
	Stale    int `json:"stale"`
	Dup      int `json:"dup"`
	Broken   int `json:"broken"`
}

// WallsReading is status.walls / status.policies.
type WallsReading struct {
	Measured bool `json:"measured"`
	// LawsRead is false when the laws in force could not be read: no
	// declaration is then judged stale or anchored by its law, only by
	// being there.
	LawsRead bool                 `json:"laws_read"`
	Repos    map[string]time.Time `json:"repos"`
	WallCounts
	ByKind   map[string]WallCounts `json:"by_kind,omitempty"`
	ByDomain map[string]WallCounts `json:"by_domain"`
	// Orphans, Stale and Broken name what to fix, "<kind> <name> (<file>:<line>)".
	Orphans    []string `json:"orphans"`
	StaleList  []string `json:"stale_list"`
	BrokenList []string `json:"broken_list"`
}

func newWallsReading() WallsReading {
	return WallsReading{Repos: map[string]time.Time{}, ByKind: map[string]WallCounts{}, ByDomain: map[string]WallCounts{},
		Orphans: []string{}, StaleList: []string{}, BrokenList: []string{}}
}

// walls gathers the kept walls of every repository: walls (points and their
// declarations) and policies (declarations only). laws are the codes in
// force (LAW-k, CORE-k); nil when they could not be read.
func (s *Scanner) walls(laws map[string]bool) (WallsReading, WallsReading) {
	w, p := newWallsReading(), newWallsReading()
	w.LawsRead, p.LawsRead = laws != nil, laws != nil
	var all []keptWalls
	for _, r := range s.repoNames() {
		raw, err := s.kept().Read(keptWallsPrefix + r)
		var k keptWalls
		if err != nil || raw == nil || json.Unmarshal(raw, &k) != nil {
			continue
		}
		all = append(all, k)
		w.Repos[r], p.Repos[r] = k.At, k.At
	}
	if len(all) == 0 {
		return w, p
	}
	w.Measured, p.Measured = true, true
	mainIDs := map[string]bool{}
	for _, k := range all {
		for _, d := range k.Decls {
			if d.Kind == "wall" && d.DupOf == "" {
				mainIDs[d.ID] = true
			}
		}
	}
	add := func(r *WallsReading, kind, domain string, f func(c *WallCounts)) {
		f(&r.WallCounts)
		if kind != "" {
			c := r.ByKind[kind]
			f(&c)
			r.ByKind[kind] = c
		}
		if domain == "" {
			domain = "unclaimed"
		}
		c := r.ByDomain[domain]
		f(&c)
		r.ByDomain[domain] = c
	}
	// judge is a declaration's state: anchored, stale, dup or broken.
	judge := func(d WallDecl) string {
		switch {
		case d.DupOf != "" && !mainIDs[d.DupOf]:
			return "broken"
		case d.DupOf != "":
			return "dup"
		case d.Law == "" || (laws != nil && !laws[d.Law]):
			return "stale"
		}
		return "anchored"
	}
	declByID := map[string]WallDecl{}
	for _, k := range all {
		for _, d := range k.Decls {
			if d.Kind == "wall" {
				if _, ok := declByID[d.ID]; !ok {
					declByID[d.ID] = d
				}
			}
		}
		for _, pt := range k.Points {
			where := pt.Kind + " " + pt.Name + " (" + k.Repo + ":" + pt.File + lineSuffix(pt.Line) + ")"
			add(&w, pt.Kind, pt.Domain, func(c *WallCounts) { c.Total++ })
			if pt.Decl == "" {
				add(&w, pt.Kind, pt.Domain, func(c *WallCounts) { c.Orphan++ })
				w.Orphans = append(w.Orphans, where)
				continue
			}
			d, ok := declByID[pt.Decl]
			if !ok {
				d = WallDecl{ID: pt.Decl}
			}
			switch judge(d) {
			case "anchored":
				add(&w, pt.Kind, pt.Domain, func(c *WallCounts) { c.Anchored++ })
			case "stale":
				add(&w, pt.Kind, pt.Domain, func(c *WallCounts) { c.Stale++ })
				w.StaleList = append(w.StaleList, where+" law "+orNone(d.Law))
			case "dup":
				add(&w, pt.Kind, pt.Domain, func(c *WallCounts) { c.Dup++ })
			case "broken":
				add(&w, pt.Kind, pt.Domain, func(c *WallCounts) { c.Broken++ })
				w.BrokenList = append(w.BrokenList, where+" dup-of "+d.DupOf)
			}
		}
		for _, d := range k.Decls {
			if d.Kind != "policy" {
				continue
			}
			dom := k.DeclDomains[d.File+":"+itoa(d.Line)]
			where := "policy " + d.ID + " (" + k.Repo + ":" + d.File + lineSuffix(d.Line) + ")"
			add(&p, "", dom, func(c *WallCounts) { c.Total++ })
			switch judge(d) {
			case "anchored":
				add(&p, "", dom, func(c *WallCounts) { c.Anchored++ })
			case "stale":
				add(&p, "", dom, func(c *WallCounts) { c.Stale++ })
				p.StaleList = append(p.StaleList, where+" law "+orNone(d.Law))
			case "dup":
				add(&p, "", dom, func(c *WallCounts) { c.Dup++ })
			case "broken":
				add(&p, "", dom, func(c *WallCounts) { c.Broken++ })
				p.BrokenList = append(p.BrokenList, where+" dup-of "+d.DupOf)
			}
		}
	}
	for _, r := range []*WallsReading{&w, &p} {
		sort.Strings(r.Orphans)
		sort.Strings(r.StaleList)
		sort.Strings(r.BrokenList)
	}
	p.ByKind = nil
	return w, p
}

func lineSuffix(n int) string {
	if n <= 0 {
		return ""
	}
	return ":" + itoa(n)
}
