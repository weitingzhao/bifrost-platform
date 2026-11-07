package rules

import (
	"regexp"
	"slices"
	"strings"
)

var InfraLandingAllow = []string{
	"agent-config/work/multi-agent/CUT-*.md",
	"agent-config/work/multi-agent/DESIGN-*.md",
	"agent-config/work/multi-agent/REPORT-*.md",
	"agent-config/work/multi-agent/REVIEW-*.md",
	"agent-config/work/multi-agent/LESSONS-*.md",
	"agent-config/work/multi-agent/reviews/*.md",
}

// infraNever are the infra paths no grant ever reaches: credentials, the
// forge's own automation and the agents' instructions. They stay absolute.
var infraNever = []string{
	"**/.env*", ".github/**", ".gitea/**", "**/.gitmodules", "**/.gitattributes",
	"**/AGENTS.md", "**/CLAUDE.md", "**/SKILL.md", "**/*.mdc", "**/.claude/**", "**/.cursor/**", "**/.codex/**",
	"**/.vscode/**", "**/.idea/**", "**/*.code-workspace",
}

// infraFloor are the other infra hard paths: refused without a grant, and
// pushable with the Owner's approve_floor_landing for exactly the pinned
// files (agent-config/** counts only outside agent-config/work/).
var infraFloor = []string{
	"k8s/**", "scripts/**", "docker/**", "config/**", "Makefile", "docker-compose*.yml", "mkdocs.yml", "requirements*.txt",
	"**/.gitignore", "agent-config/**", "**/OWNER-STEPS-*", "**/*RUNBOOK*", "**/TICKET-*", "**/REQUEST-*", "**/RULE-*",
}

// InfraNeverGrantable: p is an infra path no grant can waive.
func InfraNeverGrantable(p string) bool {
	return !landingPath(p) || landingMatches(infraNever, p)
}

// FloorEntry is the floor-grantable pattern p matches in repo, "" when p
// is not floor-grantable there. Infra's are its hard paths a grant may
// reach (infraFloor); platform's are its floor paths that are neither hard
// (core or absolute) nor open, with the PlatformLandingFloor glob matched.
func FloorEntry(repo, p string) string {
	switch repo {
	case "bifrost-trade-infra":
		return infraFloorEntry(p)
	case "bifrost-platform":
		if !LandingFloor(repo, p) || LandingHard(repo, p) || LandingOpen(repo, p) {
			return ""
		}
		for _, entry := range PlatformLandingFloor {
			if landingMatches([]string{entry}, p) {
				return entry
			}
		}
	}
	return ""
}

// InfraFloorEntry is FloorEntry for infra.
func InfraFloorEntry(p string) string { return FloorEntry("bifrost-trade-infra", p) }

// infraFloorEntry is the floor-grantable pattern an infra path matches, ""
// when the path is never-grantable or not hard at all.
func infraFloorEntry(p string) string {
	if InfraNeverGrantable(p) {
		return ""
	}
	for _, entry := range infraFloor {
		if entry == "agent-config/**" && strings.HasPrefix(p, "agent-config/work/") {
			continue
		}
		if landingMatches([]string{entry}, p) {
			return entry
		}
	}
	return ""
}

func infraHard(p string) bool {
	return InfraNeverGrantable(p) || InfraFloorEntry(p) != ""
}

// infraSecretKind is a manifest line declaring a Secret, at any spacing
// (a list item's "- kind: Secret" and a quoted value included).
var infraSecretKind = regexp.MustCompile(`^\s*(?:-\s+)?kind\s*:\s*["']?Secret["']?\s*(?:#.*)?$`)

// InfraSecretManifest: p is a k8s/** file whose content declares a Secret.
// Such a file is never-grantable, whatever else it holds (no Secret YAML
// enters a public repository).
func InfraSecretManifest(p string, content string) bool {
	if !strings.HasPrefix(p, "k8s/") {
		return false
	}
	for _, line := range strings.Split(content, "\n") {
		if infraSecretKind.MatchString(strings.TrimSuffix(line, "\r")) {
			return true
		}
	}
	return false
}

// LandingContentViolation refuses infra content no grant can waive: a k8s
// manifest declaring a Secret, in the lines the diff adds (the gate and the
// material read the whole file at R as well).
func LandingContentViolation(repo string, changes []LandingChange) (string, bool) {
	if repo != "bifrost-trade-infra" {
		return "", false
	}
	for _, c := range changes {
		if c.NewPath != "" && InfraSecretManifest(c.NewPath, c.AddedText) {
			return "secret: " + c.NewPath, true
		}
	}
	return "", false
}

// LandingFloorGrantable: a change waits for approve_floor_landing rather
// than being refused. Its soft set is floor_grant, with rule_paths only when
// every path that refuses is floor-grantable; size and core are never
// waived, and every changed path is floor-grantable (the grant pins exactly
// the files it changes, and the gate requires M0..R to change nothing else).
// On platform a path the signed rule denies stays refused.
func LandingFloorGrantable(l *Landing, repo string, changes []LandingChange, soft []LandingRefusal) bool {
	if !FloorGrantSupported(repo) || !slices.Contains(soft, LandingFloorGrant) || len(changes) == 0 {
		return false
	}
	for _, s := range soft {
		if s != LandingFloorGrant && s != LandingRulePaths {
			return false
		}
	}
	if _, bad := LandingChangeKindViolation(repo, changes); bad {
		return false
	}
	var deny []string
	if repo == "bifrost-platform" {
		r, ok := l.Repo(repo)
		if !ok {
			return false
		}
		deny = r.Deny
	}
	for _, c := range changes {
		if len(c.Paths()) == 0 {
			return false
		}
		for _, p := range c.Paths() {
			if FloorEntry(repo, p) == "" || landingMatches(deny, p) {
				return false
			}
		}
	}
	return true
}

// FloorPaths are the floor-grantable paths of a change with their entries,
// sorted by path.
func FloorPaths(repo string, changes []LandingChange) [][2]string {
	if !FloorGrantSupported(repo) {
		return nil
	}
	seen := map[string]bool{}
	var out [][2]string
	for _, c := range changes {
		for _, p := range c.Paths() {
			if e := FloorEntry(repo, p); e != "" && !seen[p] {
				seen[p] = true
				out = append(out, [2]string{p, e})
			}
		}
	}
	slices.SortFunc(out, func(a, b [2]string) int { return strings.Compare(a[0], b[0]) })
	return out
}

// Infra uses no-renames endpoint records. The explicit kind is retained by
// the trusted reader; inference keeps existing platform fixtures compatible.
func (c LandingChange) ChangeKind() string {
	if c.Kind != "" {
		return c.Kind
	}
	if c.OldPath == "" {
		return "A"
	}
	if c.NewPath == "" {
		return "D"
	}
	if c.OldPath != c.NewPath {
		return "R"
	}
	return "M"
}

func LandingStructuralViolation(repo string, changes []LandingChange) (string, bool) {
	if repo == "bifrost-trade-infra" {
		for _, c := range changes {
			if len(c.Paths()) == 0 {
				return "path_invalid", true
			}
			for _, p := range c.Paths() {
				if !landingPath(p) {
					return "path_invalid: " + p, true
				}
			}
		}
	}
	if why, bad := StructuralViolation(changes); bad {
		return why, true
	}
	if repo == "bifrost-trade-infra" {
		for _, c := range changes {
			if c.ChangeKind() == "T" {
				return "type_changed", true
			}
			for _, side := range [][2]string{{c.OldPath, c.OldMode}, {c.NewPath, c.NewMode}} {
				if side[0] != "" && side[1] != "100644" {
					return "mode", true
				}
			}
		}
	}
	return "", false
}

func LandingChangeKindViolation(repo string, changes []LandingChange) (string, bool) {
	if repo == "bifrost-trade-infra" {
		for _, c := range changes {
			if k := c.ChangeKind(); k != "A" && k != "M" {
				return k, true
			}
		}
	}
	return "", false
}

// PolicyFloorDetail preserves the first failing infra layer for audit/UI.
func PolicyFloorDetail(l *Landing, repo string, changes []LandingChange) map[string]any {
	if why, bad := LandingStructuralViolation(repo, changes); bad {
		return map[string]any{"structural": why}
	}
	if why, bad := LandingAbsoluteViolation(repo, changes); bad {
		return map[string]any{"hard": why}
	}
	if why, bad := LandingContentViolation(repo, changes); bad {
		return map[string]any{"secret": strings.TrimPrefix(why, "secret: ")}
	}
	if why, bad := LandingChangeKindViolation(repo, changes); bad {
		return map[string]any{"change_kind": why}
	}
	soft := LandingSoftRefusals(l, repo, changes)
	if len(soft) == 0 {
		return nil
	}
	items := make([]string, len(soft))
	for i, s := range soft {
		items[i] = string(s)
	}
	detail := map[string]any{"soft": items, "grant_supported": GrantSupported(repo)}
	if fp := FloorPaths(repo, changes); len(fp) > 0 {
		paths := make([]any, len(fp))
		for i, e := range fp {
			paths[i] = map[string]any{"path": e[0], "entry": e[1]}
		}
		detail["floor_paths"] = paths
	}
	return detail
}
