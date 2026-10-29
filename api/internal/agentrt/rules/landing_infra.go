package rules

import "strings"

var InfraLandingAllow = []string{
	"agent-config/work/multi-agent/CUT-*.md",
	"agent-config/work/multi-agent/DESIGN-*.md",
	"agent-config/work/multi-agent/REPORT-*.md",
	"agent-config/work/multi-agent/REVIEW-*.md",
	"agent-config/work/multi-agent/LESSONS-*.md",
	"agent-config/work/multi-agent/reviews/*.md",
}

func infraHard(p string) bool {
	if strings.HasPrefix(p, "agent-config/") && !strings.HasPrefix(p, "agent-config/work/") {
		return true
	}
	return landingMatches([]string{
		"k8s/**", "scripts/**", "docker/**", "config/**", "Makefile", "docker-compose*.yml", "mkdocs.yml", "requirements*.txt", ".github/**", ".gitea/**",
		"**/.gitattributes", "**/.gitmodules", "**/.gitignore", "**/.vscode/**", "**/.idea/**", "**/*.code-workspace",
		"**/AGENTS.md", "**/CLAUDE.md", "**/SKILL.md", "**/*.mdc", "**/.claude/**", "**/.cursor/**", "**/.codex/**",
		"**/OWNER-STEPS-*", "**/*RUNBOOK*", "**/TICKET-*", "**/REQUEST-*", "**/RULE-*", "**/.env*",
	}, p)
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
	return map[string]any{"soft": items, "grant_supported": GrantSupported(repo)}
}
