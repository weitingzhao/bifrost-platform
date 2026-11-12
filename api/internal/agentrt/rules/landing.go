package rules

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

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
	Limits      *LandingRepoLimits
}
type LandingRepoLimits struct{ MaxFiles, MaxChangedLines, MaxInFlight int }

// LandingInFlightCeiling bounds repository landing capacity.
const LandingInFlightCeiling = 8

// CheckedLimits reports effective capacities. Legacy size fields remain in the
// signed document but have no effect.
func (l *Landing) CheckedLimits(repo string) LandingLimits {
	v := l.EffectiveLimits(repo)
	v.MaxFiles, v.MaxChangedLines = 0, 0
	return v
}

func (l *Landing) EffectiveLimits(repo string) LandingLimits {
	v := l.Limits
	if r, ok := l.Repo(repo); ok && r.Limits != nil {
		v.MaxFiles = r.Limits.MaxFiles
		v.MaxChangedLines = r.Limits.MaxChangedLines
		v.MaxInFlightPerRepo = r.Limits.MaxInFlight
	}
	return v
}

// LegacyMaxBatch and LegacyMaxLandingsPerDay are limits.max_batch and
// limits.max_landings_per_day as a signed rule states them (0 when left
// out). They bound nothing (cleanup wave 1, 2026-11-11): they are read and
// kept only so a rule signed with them still has its canonical body.
type LandingLimits struct {
	MaxFiles, MaxChangedLines, LegacyMaxLandingsPerDay, MaxInFlightPerRepo, LegacyMaxBatch int
	// legacyQuotaStated: the text states max_landings_per_day, so the
	// canonical body keeps it (a rule without it never gains it).
	legacyQuotaStated bool
}

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
		if !LandingRepoSupported(repo.Repo) || seen[repo.Repo] {
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

		if repo.Repo == "bifrost-trade-infra" && repo.STGRelease != "none" {
			return fmt.Errorf("infra stg_release must be none")
		}
		node, err := p.node("limits", true)
		if err != nil {
			return err
		}
		if node != nil {
			lim, err := mapping(node, "repo limits")
			if err != nil {
				return err
			}
			repo.Limits = &LandingRepoLimits{}
			for key, dst := range map[string]*int{"max_files": &repo.Limits.MaxFiles, "max_changed_lines": &repo.Limits.MaxChangedLines, "max_in_flight": &repo.Limits.MaxInFlight} {
				upper := 1 << 30
				if key == "max_in_flight" {
					upper = LandingInFlightCeiling
				}
				lower := 0
				if key == "max_in_flight" {
					lower = 1
				} else if _, present := lim.fields[key]; !present {
					continue
				}
				if *dst, err = lim.integer(key, lower, upper); err != nil {
					return err
				}
			}
			if err := lim.done(); err != nil {
				return err
			}
		}
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
	for key, dst := range map[string]*int{"max_files": &l.Limits.MaxFiles, "max_changed_lines": &l.Limits.MaxChangedLines, "max_landings_per_day": &l.Limits.LegacyMaxLandingsPerDay, "max_in_flight_per_repo": &l.Limits.MaxInFlightPerRepo} {
		max := 1 << 30
		if key == "max_in_flight_per_repo" {
			max = LandingInFlightCeiling
		}
		lower := 0
		if key == "max_in_flight_per_repo" {
			lower = 1
		} else if _, present := lim.fields[key]; !present {
			// Size fields and the legacy daily quota may be left out.
			continue
		}
		if key == "max_landings_per_day" {
			l.Limits.legacyQuotaStated = true
		}
		if *dst, err = lim.integer(key, lower, max); err != nil {
			return err
		}
	}
	// Legacy: accepted for rules signed with it, no effect.
	if _, ok := lim.fields["max_batch"]; ok {
		if l.Limits.LegacyMaxBatch, err = lim.integer("max_batch", 1, 1<<30); err != nil {
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
		row := map[string]any{"repo": r.Repo, "allow": anyList(sortedLandingAllow(r.Allow)), "deny": anyList(r.Deny), "stg_release": r.STGRelease}
		if r.Limits != nil {
			row["limits"] = map[string]any{"max_files": r.Limits.MaxFiles, "max_changed_lines": r.Limits.MaxChangedLines, "max_in_flight": r.Limits.MaxInFlight}
		}
		repos = append(repos, row)
	}
	limits := map[string]any{"max_files": l.Limits.MaxFiles, "max_changed_lines": l.Limits.MaxChangedLines, "max_in_flight_per_repo": l.Limits.MaxInFlightPerRepo}
	if l.Limits.legacyQuotaStated {
		limits["max_landings_per_day"] = l.Limits.LegacyMaxLandingsPerDay
	}
	if l.Limits.LegacyMaxBatch > 0 {
		limits["max_batch"] = l.Limits.LegacyMaxBatch
	}
	return map[string]any{"tenant": l.Tenant, "repos": repos, "limits": limits}
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
		a, b := p.EffectiveLimits(r.Repo), n.EffectiveLimits(r.Repo)
		if b.MaxFiles > a.MaxFiles || b.MaxChangedLines > a.MaxChangedLines || b.MaxInFlightPerRepo > a.MaxInFlightPerRepo {
			return fmt.Errorf("landing repo limit goes up")
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
	return utf8.ValidString(p) && strings.IndexFunc(p, unicode.IsControl) < 0 && p != "" && p != "." && !strings.HasPrefix(p, "/") && path.Clean(p) == p && !strings.ContainsAny(p, "\\\x00\r\n") && !slices.Contains(strings.Split(p, "/"), "..")
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

// PlatformLandingFloor are platform's floor globs. A path matching one that
// is neither hard nor open is floor-grantable with the glob it matches as
// its entry (FloorEntry); hard and open paths are judged by their own tables.
var PlatformLandingFloor = []string{"api/**", "console/vite.config.*", "console/tsconfig*", "console/index.html", "console/src/main.tsx", "console/src/App.tsx", "console/src/api/**", "console/src/lib/**", "console/src/hooks/**", "console/src/pwa/**", "mcp/**", "agent/**", "scripts/**", "config/**", "Makefile", ".husky/**"}

// Change comes from a trusted diff reader, not an executor's report. Modes
// are git's six-digit modes; an absent side is empty (add/delete).
type LandingChange struct {
	Kind                               string
	OldPath, NewPath, OldMode, NewMode string
	Added, Deleted                     int
	AddedText                          string
	Binary                             bool
}

var presentationImport = regexp.MustCompile(`(?s)\bimport\b[^;]*?\bfrom\s*["'](?:[^"']*/)?(?:api|lib|hooks)/`)
var presentationEffect = regexp.MustCompile(`\bfetch\s*\(|XMLHttpRequest|useQuery|useMutation|window\s*\.\s*location`)

// LandingPresentationOnly implements CUT §2.3's three display checks without
// I/O. It is deliberately separate from the immutable floor.
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

// Keep the signed canonical form stable while retaining the stated pattern order.
func sortedLandingAllow(allow []string) []string {
	out := slices.Clone(allow)
	slices.Sort(out)
	return out
}
