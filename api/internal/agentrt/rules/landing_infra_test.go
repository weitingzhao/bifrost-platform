package rules

import (
	"strings"
	"testing"
)

func TestInfraHardClosure(t *testing.T) {
	t.Parallel()
	for _, p := range []string{
		"agent-config/agentrt-rules/landing-dev.yaml", "agent-config/landing/doc-consumers.yaml",
		"scripts/landing/doc_policy.py", "scripts/landing/doc_consumers.py", "scripts/landing/coverage_infra.py", "scripts/landing/ci-infra.sh",
		"k8s/cicd/tekton/pipeline-ci-infra.yaml", "k8s/cicd/tekton/task-ci-infra-clone.yaml", "k8s/cicd/tekton/pipeline-build-ci-infra-tools.yaml", "k8s/cicd/tekton/trigger-trade-ci.yaml", "k8s/cicd/tekton/rbac-trigger.yaml",
		"docker/ci-infra/Dockerfile", "docker/ci-infra/tools.lock", "agent-config/scripts/code-health/new-scanner.sh", "agent-config/scripts/check-agent-config-parity.sh",
		"scripts/gitea/new-check.sh", "scripts/release/push-main.sh", "agent-config/release-policy/policy.yaml", "scripts/release/db-steps.d/step.sql", "agent-config/TECH_DEBT.md", "agent-config/WORK.md",
		".gitattributes", "x/.gitmodules", "x/.gitignore", "x/.vscode/tasks.json", "x/.idea/run.xml", "x/a.code-workspace",
		"agent-config/work/multi-agent/reviews/SKILL.md", "agent-config/work/multi-agent/DESIGN-RUNBOOK-x.md", "x/AGENTS.md", "x/CLAUDE.md", "x/a.mdc", "x/.claude/a", "x/.cursor/a", "x/.codex/a", "x/OWNER-STEPS-a.md", "x/TICKET-a.md", "x/REQUEST-a.md", "x/RULE-a.md", "x/.env.local",
		"Makefile", "config/x", "docker-compose.prod.yml", "mkdocs.yml", "requirements-dev.txt", ".github/workflows/a.yml", ".gitea/workflows/a.yml",
	} {
		if !LandingHardAbsolute("bifrost-trade-infra", p) || !LandingHard("bifrost-trade-infra", p) || LandingHardCore("bifrost-trade-infra", p) {
			t.Errorf("unprotected infra dependency: %s", p)
		}
	}
	for _, p := range []string{"api/internal/delivery/new_file.go", "api/internal/agentrt/infracheck/new_file.go", "api/internal/agentrt/store/repos.go", "api/cmd/infracheck/main.go"} {
		if !LandingHard("bifrost-platform", p) {
			t.Errorf("unprotected platform dependency: %s", p)
		}
	}
	for _, p := range []string{"agent-config/work/multi-agent/REPORT-a.md", "agent-config/work/multi-agent/reviews/r1.md"} {
		if LandingHard("bifrost-trade-infra", p) {
			t.Fatal("control", p)
		}
	}
}

// T16 is an inner parser test: Git cannot create dot/traversal endpoints.
func TestInfraPathParser(t *testing.T) {
	t.Parallel()
	good := LandingChange{NewPath: "agent-config/work/multi-agent/REPORT-a.md", NewMode: "100644", Added: 1}
	if _, bad := LandingStructuralViolation("bifrost-trade-infra", []LandingChange{good}); bad {
		t.Fatal("control")
	}
	for _, p := range []string{"../x.md", "/x.md", "a/./b.md", "a\x00b", "", string([]byte{0xff})} {
		c := good
		c.NewPath = p
		if why, bad := LandingStructuralViolation("bifrost-trade-infra", []LandingChange{c}); !bad || !strings.HasPrefix(why, "path_invalid") {
			t.Fatalf("%q: %s %v", p, why, bad)
		}
	}
}

const infraRuleSection = `  - repo: bifrost-trade-infra
    allow: ["agent-config/work/multi-agent/REPORT-*.md"]
    deny: []
    stg_release: none
    limits: {max_files: 5, max_changed_lines: 1500, max_in_flight: 1}
`

func TestS6T72EffectiveRepoLimits(t *testing.T) {
	t.Parallel()
	text := strings.Replace(landingYAML, "limits:\n", infraRuleSection+"limits:\n", 1)
	text = strings.Replace(text, "deny: []", "deny: [\"console/src/pages/Private.tsx\"]", 1)
	text = strings.Replace(text, "stg_release: bifrost-deliver-platform", "stg_release: none", 1)
	parse := func(text string) Rule {
		t.Helper()
		r, err := Parse([]byte(text))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	old := parse(text)
	if old.Landing.EffectiveLimits("bifrost-platform").MaxChangedLines != 300 || old.Landing.EffectiveLimits("bifrost-trade-infra").MaxChangedLines != 1500 {
		t.Fatal("inheritance")
	}
	b, err := old.BodyJSON()
	if err != nil {
		t.Fatal(err)
	}
	round := parse(string(b))
	b2, _ := round.BodyJSON()
	if string(b) != string(b2) {
		t.Fatal("unstable rule body")
	}
	for _, tc := range []struct {
		from, to string
		tight    bool
	}{
		{"max_changed_lines: 1500", "max_changed_lines: 1000", true},
		{"max_changed_lines: 300", "max_changed_lines: 400", false},
		{"deny: [\"console/src/pages/Private.tsx\"]", "deny: []", false},
		{"stg_release: none", "stg_release: bifrost-deliver-platform", false},
	} {
		next := parse(strings.Replace(text, tc.from, tc.to, 1))
		if err := Tightens(old, next); (err == nil) != tc.tight {
			t.Fatalf("%s -> %s: %v", tc.from, tc.to, err)
		}
	}
	if err := Tightens(landingRule(t), old); err == nil {
		t.Fatal("adding infra is expansion requiring signature")
	}
	if _, err := Parse([]byte(strings.Replace(text, "repo: bifrost-trade-infra", "repo: bifrost-research", 1))); err == nil {
		t.Fatal("unknown repo")
	}
}
