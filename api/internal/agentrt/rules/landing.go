package rules

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

const KindLanding = "landing"

type Landing struct {
	Tenant string
	Repos  []LandingRepo
	Limits LandingLimits
}
type LandingRepo struct {
	Repo        string
	Allow, Deny []string
	STGRelease  string
}
type LandingLimits struct{ MaxFiles, MaxChangedLines, MaxLandingsPerDay, MaxInFlightPerRepo int }

func (r *Rule) landing(o *object, arg string) error {
	l := &Landing{}
	var err error
	if l.Tenant, err = tenant(o, arg); err != nil {
		return err
	}
	seen := map[string]bool{}
	err = o.list("repos", false, func(n *yaml.Node, where string) error {
		p, err := mapping(n, where)
		if err != nil {
			return err
		}
		var repo LandingRepo
		if repo.Repo, err = p.str("repo", false); err != nil {
			return err
		}
		// The first slice only authorizes platform. Infra needs S6's CI and floor.
		if repo.Repo != "bifrost-platform" || seen[repo.Repo] {
			return fmt.Errorf("landing repo is unsupported or repeated: %s", repo.Repo)
		}
		seen[repo.Repo] = true
		if repo.Allow, err = p.strings("allow", false); err != nil {
			return err
		}
		if repo.Deny, err = p.strings("deny", false); err != nil {
			return err
		}
		for _, patterns := range [][]string{repo.Allow, repo.Deny} {
			for _, pattern := range patterns {
				if _, err := landingGlob(pattern); err != nil {
					return err
				}
			}
		}
		if repo.STGRelease, err = p.str("stg_release", false); err != nil {
			return err
		}
		if repo.STGRelease != "none" && repo.STGRelease != "bifrost-deliver-platform" {
			return fmt.Errorf("unsupported stg_release")
		}
		slices.Sort(repo.Allow)
		slices.Sort(repo.Deny)
		l.Repos = append(l.Repos, repo)
		return p.done()
	})
	if err != nil {
		return err
	}
	lim, err := o.object("limits")
	if err != nil {
		return err
	}
	for key, dst := range map[string]*int{"max_files": &l.Limits.MaxFiles, "max_changed_lines": &l.Limits.MaxChangedLines, "max_landings_per_day": &l.Limits.MaxLandingsPerDay, "max_in_flight_per_repo": &l.Limits.MaxInFlightPerRepo} {
		max := 1 << 30
		if key == "max_in_flight_per_repo" {
			max = 1
		}
		if *dst, err = lim.integer(key, 0, max); err != nil {
			return err
		}
	}
	if err := lim.done(); err != nil {
		return err
	}
	r.Landing = l
	return nil
}
func (l *Landing) doc() map[string]any {
	repos := []any{}
	for _, r := range l.Repos {
		repos = append(repos, map[string]any{"repo": r.Repo, "allow": anyList(r.Allow), "deny": anyList(r.Deny), "stg_release": r.STGRelease})
	}
	return map[string]any{"tenant": l.Tenant, "repos": repos, "limits": map[string]any{"max_files": l.Limits.MaxFiles, "max_changed_lines": l.Limits.MaxChangedLines, "max_landings_per_day": l.Limits.MaxLandingsPerDay, "max_in_flight_per_repo": l.Limits.MaxInFlightPerRepo}}
}
func (l *Landing) Repo(name string) (LandingRepo, bool) {
	if l != nil {
		for _, r := range l.Repos {
			if r.Repo == name {
				return r, true
			}
		}
	}
	return LandingRepo{}, false
}
func landingTightens(p, n *Landing) error {
	if p == nil || n == nil || p.Tenant != n.Tenant {
		return fmt.Errorf("landing tenant changes")
	}
	for _, r := range n.Repos {
		old, ok := p.Repo(r.Repo)
		if !ok {
			return fmt.Errorf("landing gains repo")
		}
		if err := subset(r.Allow, old.Allow, "allow"); err != nil {
			return err
		}
		if err := subset(old.Deny, r.Deny, "deny removed"); err != nil {
			return err
		}
		if r.STGRelease != old.STGRelease && r.STGRelease != "none" {
			return fmt.Errorf("stg_release expands")
		}
	}
	a, b := p.Limits, n.Limits
	if b.MaxFiles > a.MaxFiles || b.MaxChangedLines > a.MaxChangedLines || b.MaxLandingsPerDay > a.MaxLandingsPerDay || b.MaxInFlightPerRepo > a.MaxInFlightPerRepo {
		return fmt.Errorf("landing limit goes up")
	}
	return nil
}

// Only rooted, canonical slash paths are accepted. ** is a whole component;
// **/ matches zero or more directory components. No filesystem is consulted.
func landingGlob(pattern string) (*regexp.Regexp, error) {
	if !landingPath(pattern) {
		return nil, fmt.Errorf("invalid landing glob %q", pattern)
	}
	var out strings.Builder
	out.WriteString("^")
	parts := strings.Split(pattern, "/")
	for i, p := range parts {
		if p == "**" {
			if i == len(parts)-1 {
				out.WriteString(".*")
			} else {
				out.WriteString("(?:[^/]+/)*")
			}
			continue
		}
		if strings.Contains(p, "**") {
			return nil, fmt.Errorf("** must be a whole path component")
		}
		for _, c := range p {
			if c == '*' {
				out.WriteString("[^/]*")
			} else {
				out.WriteString(regexp.QuoteMeta(string(c)))
			}
		}
		if i < len(parts)-1 {
			out.WriteByte('/')
		}
	}
	out.WriteString("$")
	return regexp.Compile(out.String())
}
func landingPath(p string) bool {
	return strings.IndexFunc(p, unicode.IsControl) < 0 && p != "" && p != "." && !strings.HasPrefix(p, "/") && path.Clean(p) == p && !strings.ContainsAny(p, "\\\x00\r\n") && !slices.Contains(strings.Split(p, "/"), "..")
}
func landingMatches(patterns []string, p string) bool {
	for _, pattern := range patterns {
		r, err := landingGlob(pattern)
		if err == nil && r.MatchString(p) {
			return true
		}
	}
	return false
}

// LandingFloor is immutable policy, independent of the signed allow list.
func LandingFloor(repo, p string) bool {
	if repo != "bifrost-platform" || !landingPath(p) {
		return true
	}
	b := path.Base(p)
	if strings.HasPrefix(b, ".env") || slices.Contains([]string{"go.mod", "go.sum", "package.json", "package-lock.json"}, b) {
		return true
	}
	return landingMatches([]string{"api/**", "console/vite.config.*", "console/tsconfig*", "console/index.html", "console/src/main.tsx", "console/src/App.tsx", "console/src/api/**", "console/src/lib/**", "console/src/hooks/**", "console/src/pwa/**", "mcp/**", "agent/**", "scripts/**", "config/**", "Makefile", ".husky/**"}, p)
}

// Change comes from a trusted diff reader, not an executor's report. Modes
// are git's six-digit modes; an absent side is empty (add/delete).
type LandingChange struct {
	OldPath, NewPath, OldMode, NewMode string
	Added, Deleted                     int
	AddedText                          string
	Binary                             bool
}

var presentationImport = regexp.MustCompile(`(?s)\bimport\b[^;]*?\bfrom\s*["'](?:[^"']*/)?(?:api|lib|hooks)/`)
var presentationEffect = regexp.MustCompile(`\bfetch\s*\(|XMLHttpRequest|useQuery|useMutation|window\s*\.\s*location`)

// LandingPresentationOnly implements CUT §2.3's three display checks without
// I/O. It is deliberately separate from the immutable floor and size limits.
// Both sides of a rename and the old path of a deletion are changed paths.
func LandingPresentationOnly(allow []string, changes []LandingChange) bool {
	if len(changes) == 0 {
		return false
	}
	for _, c := range changes {
		if c.OldPath == "" && c.NewPath == "" {
			return false
		}
		for _, p := range c.Paths() {
			if p != "" && (!landingPath(p) || !landingMatches(allow, p)) {
				return false
			}
		}
		if presentationImport.MatchString(c.AddedText) || presentationEffect.MatchString(c.AddedText) {
			return false
		}
	}
	return true
}

// FloorViolation is the pre-grant compatibility gate. New consumers check
// StructuralViolation and LandingAbsoluteViolation first, then the refusal
// set. Keeping the old floor here prevents unwired v1 callers granting access.
func FloorViolation(repo string, changes []LandingChange) (string, bool) {
	if why, bad := StructuralViolation(changes); bad {
		return why, true
	}
	if why, bad := LandingAbsoluteViolation(repo, changes); bad {
		return "floor: " + why, true
	}
	for _, c := range changes {
		for _, p := range c.Paths() {
			if LandingFloor(repo, p) {
				return "floor: " + p, true
			}
		}
	}
	return "", false
}

func (l *Landing) CheckChanges(repo string, changes []LandingChange) error {
	return legacyCheckChanges(l, repo, changes)
}
