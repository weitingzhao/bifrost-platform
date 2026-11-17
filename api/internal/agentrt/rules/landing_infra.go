package rules

import (
	"strings"
)

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
		if LandingHard(repo, p) || LandingOpen(repo, p) {
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
