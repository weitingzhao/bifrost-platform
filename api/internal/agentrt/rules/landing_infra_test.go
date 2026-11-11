package rules

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestInfraConfiguredFileLimit(t *testing.T) {
	t.Parallel()
	const repo = "bifrost-trade-infra"
	for _, tc := range []struct {
		limit, count int
		oversized    bool
	}{{100, 10, false}, {100, 99, false}, {100, 100, false}, {100, 101, true}, {5, 5, false}, {5, 6, true}} {
		t.Run(fmt.Sprintf("limit%d/files%d", tc.limit, tc.count), func(t *testing.T) {
			l := &Landing{Repos: []LandingRepo{{Repo: repo, Allow: InfraLandingAllow,
				Limits: &LandingRepoLimits{MaxFiles: tc.limit, MaxChangedLines: 1500, MaxInFlight: 1}}},
				Limits: LandingLimits{MaxFiles: 15, MaxChangedLines: 300}}
			changes := make([]LandingChange, tc.count)
			for i := range changes {
				changes[i] = LandingChange{NewPath: fmt.Sprintf("k8s/test-%d.yaml", i), NewMode: "100644", Added: 1}
			}
			soft := LandingSoftRefusals(l, repo, changes)
			if slices.Contains(soft, LandingRefusal("size")) {
				t.Fatalf("configured limit not respected: %v", soft)
			}
			if !slices.Contains(soft, LandingFloorGrant) || !LandingFloorGrantable(l, repo, changes, soft) {
				t.Fatalf("floor approval boundary changed: %v", soft)
			}
		})
	}
}

func TestInfraDisplayedClampMatchesEnforcement(t *testing.T) {
	l := &Landing{Repos: []LandingRepo{{Repo: "bifrost-trade-infra", Allow: InfraLandingAllow}}, Limits: LandingLimits{MaxFiles: 100, MaxChangedLines: 3000}}
	for _, lines := range []int{1499, 1500, 1501} {
		changes := []LandingChange{{NewPath: "k8s/example.yaml", NewMode: "100644", Added: lines}}
		refused := slices.Contains(LandingSoftRefusals(l, "bifrost-trade-infra", changes), LandingRefusal("size"))
		if refused || l.CheckedLimits("bifrost-trade-infra").MaxChangedLines != 0 {
			t.Fatalf("displayed clamp disagrees at %d lines", lines)
		}
	}
}

func TestInfraHardClosure(t *testing.T) {
	t.Parallel()
	// Floor-grantable: hard (no automatic landing), never core, not absolute,
	// each with the entry it matched.
	for p, entry := range map[string]string{
		"agent-config/agentrt-rules/landing-dev.yaml": "agent-config/**", "agent-config/landing/doc-consumers.yaml": "agent-config/**",
		"scripts/landing/doc_policy.py": "scripts/**", "scripts/landing/doc_consumers.py": "scripts/**", "scripts/landing/coverage_infra.py": "scripts/**", "scripts/landing/ci-infra.sh": "scripts/**",
		"k8s/cicd/tekton/pipeline-ci-infra.yaml": "k8s/**", "k8s/cicd/tekton/task-ci-infra-clone.yaml": "k8s/**", "k8s/cicd/tekton/pipeline-build-ci-infra-tools.yaml": "k8s/**",
		"k8s/cicd/tekton/trigger-trade-ci.yaml": "k8s/**", "k8s/cicd/tekton/rbac-trigger.yaml": "k8s/**",
		"docker/ci-infra/Dockerfile": "docker/**", "docker/ci-infra/tools.lock": "docker/**", "agent-config/scripts/code-health/new-scanner.sh": "agent-config/**",
		"agent-config/scripts/check-agent-config-parity.sh": "agent-config/**", "scripts/gitea/new-check.sh": "scripts/**", "scripts/release/push-main.sh": "scripts/**",
		"agent-config/release-policy/policy.yaml": "agent-config/**", "scripts/release/db-steps.d/step.sql": "scripts/**", "agent-config/TECH_DEBT.md": "agent-config/**", "agent-config/WORK.md": "agent-config/**",
		"x/.gitignore": "**/.gitignore", ".gitignore": "**/.gitignore", "agent-config/work/multi-agent/DESIGN-RUNBOOK-x.md": "**/*RUNBOOK*", "x/OWNER-STEPS-a.md": "**/OWNER-STEPS-*",
		"x/TICKET-a.md": "**/TICKET-*", "x/REQUEST-a.md": "**/REQUEST-*", "x/RULE-a.md": "**/RULE-*",
		"Makefile": "Makefile", "config/x": "config/**", "docker-compose.prod.yml": "docker-compose*.yml", "mkdocs.yml": "mkdocs.yml", "requirements-dev.txt": "requirements*.txt",
	} {
		if LandingHardAbsolute("bifrost-trade-infra", p) || !LandingHard("bifrost-trade-infra", p) || LandingHardCore("bifrost-trade-infra", p) || !LandingFloor("bifrost-trade-infra", p) {
			t.Errorf("floor-grantable path misclassified: %s", p)
		}
		if got := InfraFloorEntry(p); got != entry {
			t.Errorf("%s: entry %q, want %q", p, got, entry)
		}
	}
	// Never-grantable: absolute, as before, and no floor entry.
	for _, p := range []string{
		".gitattributes", "x/.gitmodules", "x/.vscode/tasks.json", "x/.idea/run.xml", "x/a.code-workspace",
		"agent-config/work/multi-agent/reviews/SKILL.md", "x/AGENTS.md", "x/CLAUDE.md", "CLAUDE.md", "x/a.mdc", "x/.claude/a", "x/.cursor/a", "x/.codex/a", "x/.env.local",
		".github/workflows/a.yml", ".gitea/workflows/a.yml", "k8s/.env", "scripts/AGENTS.md", "agent-config/.claude/settings.json", "../x.md",
	} {
		if !LandingHardAbsolute("bifrost-trade-infra", p) || !LandingHard("bifrost-trade-infra", p) || LandingHardCore("bifrost-trade-infra", p) || InfraFloorEntry(p) != "" {
			t.Errorf("unprotected never-grantable infra path: %s", p)
		}
	}
	for _, p := range []string{"api/internal/agentrt/landingci/new_file.go", "api/internal/agentrt/infracheck/new_file.go", "api/internal/agentrt/store/repos.go", "api/cmd/infracheck/main.go"} {
		if !LandingHard("bifrost-platform", p) {
			t.Errorf("unprotected platform dependency: %s", p)
		}
	}
	for _, p := range []string{"agent-config/work/multi-agent/REPORT-a.md", "agent-config/work/multi-agent/reviews/r1.md"} {
		if LandingHard("bifrost-trade-infra", p) || InfraFloorEntry(p) != "" {
			t.Fatal("control", p)
		}
	}
}

func TestInfraSoftFloorGrant(t *testing.T) {
	t.Parallel()
	l := &Landing{Repos: []LandingRepo{{Repo: "bifrost-trade-infra", Allow: InfraLandingAllow, Limits: &LandingRepoLimits{MaxFiles: 5, MaxChangedLines: 1500, MaxInFlight: 1}}},
		Limits: LandingLimits{MaxFiles: 10, MaxChangedLines: 300}}
	k8s := LandingChange{OldPath: "k8s/a.yaml", NewPath: "k8s/a.yaml", OldMode: "100644", NewMode: "100644", Added: 3}
	soft := LandingSoftRefusals(l, "bifrost-trade-infra", []LandingChange{k8s})
	if !slices.Equal(soft, []LandingRefusal{LandingFloorGrant, LandingRulePaths}) || !LandingFloorGrantable(l, "bifrost-trade-infra", []LandingChange{k8s}, soft) {
		t.Fatal("k8s", soft)
	}
	big := k8s
	big.Added = 1501
	soft = LandingSoftRefusals(l, "bifrost-trade-infra", []LandingChange{big})
	if slices.Contains(soft, LandingRefusal("size")) || !LandingFloorGrantable(l, "bifrost-trade-infra", []LandingChange{big}, soft) {
		t.Fatal("size waived", soft)
	}
	other := LandingChange{NewPath: "notes/x.md", NewMode: "100644", Added: 1}
	soft = LandingSoftRefusals(l, "bifrost-trade-infra", []LandingChange{other})
	if !slices.Equal(soft, []LandingRefusal{LandingRulePaths}) || LandingFloorGrantable(l, "bifrost-trade-infra", []LandingChange{other}, soft) {
		t.Fatal("ordinary path refusal", soft)
	}
	if LandingFloorGrantable(l, "bifrost-platform", []LandingChange{k8s}, []LandingRefusal{LandingFloorGrant}) {
		t.Fatal("platform floor grant")
	}
	if !LandingWaivesCover([]LandingRefusal{LandingFloorGrant, LandingRulePaths}, []LandingRefusal{LandingFloorGrant, LandingRulePaths}) {
		t.Fatal("floor_grant is a known category")
	}
	// Platform paths do not change.
	if !LandingHard("bifrost-platform", "Makefile") || LandingHardAbsolute("bifrost-platform", "Makefile") || !LandingHardCore("bifrost-platform", "Makefile") {
		t.Fatal("platform Makefile changed")
	}
}

func TestPlatformSoftFloorGrant(t *testing.T) {
	t.Parallel()
	const repo = "bifrost-platform"
	ts := LandingChange{OldPath: "api/internal/testscope/x.go", NewPath: "api/internal/testscope/x.go", OldMode: "100644", NewMode: "100644", Added: 3, Deleted: 1}
	ci := LandingChange{NewPath: "scripts/ci/y.sh", NewMode: "100644", Added: 2}
	for p, entry := range map[string]string{
		"api/internal/testscope/x.go": "api/**", "scripts/ci/y.sh": "scripts/**", "console/src/lib/x.ts": "console/src/lib/**", "mcp/other/a.go": "mcp/**",
		"api/internal/agentrt/rules/x.go": "", "Makefile": "", "api/internal/cluster/x.go": "", "x/.env.local": "", "console/src/components/A.tsx": "",
	} {
		if got := FloorEntry(repo, p); got != entry {
			t.Errorf("%s: entry %q, want %q", p, got, entry)
		}
	}
	for _, p := range []string{"k8s/a.yaml", "scripts/release/push-main.sh", "agent-config/WORK.md", "CLAUDE.md"} {
		if FloorEntry("bifrost-trade-infra", p) != InfraFloorEntry(p) {
			t.Errorf("infra entry changed: %s", p)
		}
	}
	changes := []LandingChange{ts, ci}
	l := landingRule(t).Landing
	soft := LandingSoftRefusals(l, repo, changes)
	if !slices.Equal(soft, []LandingRefusal{LandingFloorGrant, LandingRulePaths}) || !LandingFloorGrantable(l, repo, changes, soft) {
		t.Fatal("outside allow", soft)
	}
	if fp := FloorPaths(repo, changes); !slices.Equal(fp, [][2]string{{"api/internal/testscope/x.go", "api/**"}, {"scripts/ci/y.sh", "scripts/**"}}) {
		t.Fatal("floor paths", fp)
	}
	l.Repos[0].Allow = []string{"**"}
	if soft = LandingSoftRefusals(l, repo, changes); !slices.Equal(soft, []LandingRefusal{LandingFloorGrant}) || !LandingFloorGrantable(l, repo, changes, soft) {
		t.Fatal("inside allow", soft)
	}
	core := append(slices.Clone(changes), LandingChange{OldPath: "api/internal/agentrt/rules/x.go", NewPath: "api/internal/agentrt/rules/x.go", OldMode: "100644", NewMode: "100644", Added: 1})
	if soft = LandingSoftRefusals(l, repo, core); !slices.Equal(soft, []LandingRefusal{LandingCore, LandingFloorGrant}) || LandingFloorGrantable(l, repo, core, soft) {
		t.Fatal("core", soft)
	}
	open := LandingChange{NewPath: "api/internal/cluster/x.go", NewMode: "100644", Added: 1}
	if soft = LandingSoftRefusals(l, repo, []LandingChange{open}); len(soft) != 0 || LandingFloorGrantable(l, repo, []LandingChange{open}, []LandingRefusal{LandingFloorGrant}) {
		t.Fatal("open alone", soft)
	}
	withOpen := append(slices.Clone(changes), open)
	if soft = LandingSoftRefusals(l, repo, withOpen); !slices.Equal(soft, []LandingRefusal{LandingFloorGrant}) || LandingFloorGrantable(l, repo, withOpen, soft) {
		t.Fatal("open path beside floor paths is pinned by no floor grant", soft)
	}
	absolute := []LandingChange{{NewPath: "scripts/.env.local", NewMode: "100644", Added: 1}}
	if _, bad := LandingAbsoluteViolation(repo, absolute); !bad || LandingFloorGrantable(l, repo, absolute, []LandingRefusal{LandingFloorGrant}) {
		t.Fatal("hard_absolute")
	}
	big := slices.Clone(changes)
	big[0].Added = 301
	if soft = LandingSoftRefusals(l, repo, big); slices.Contains(soft, LandingRefusal("size")) || !LandingFloorGrantable(l, repo, big, soft) {
		t.Fatal("size waived", soft)
	}
	l.Repos[0].Deny = []string{"scripts/ci/**"}
	if soft = LandingSoftRefusals(l, repo, changes); !slices.Equal(soft, []LandingRefusal{LandingFloorGrant, LandingRulePaths}) || LandingFloorGrantable(l, repo, changes, soft) {
		t.Fatal("deny waived", soft)
	}
	if LandingFloorGrantable(nil, repo, changes, []LandingRefusal{LandingFloorGrant}) {
		t.Fatal("no rule")
	}
}

func TestInfraSecretManifest(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]bool{
		"kind: Secret\n": true, "  kind:Secret": true, "kind :\tSecret  ": true, "- kind: Secret": true, "kind: 'Secret'": true, "kind: Secret # db\n": true,
		"kind: Secrets": false, "kind: SecretProviderClass": false, "# kind: Secret": false, "metadata:\n  name: Secret": false, "kind: ConfigMap": false,
	} {
		if got := InfraSecretManifest("k8s/a.yaml", text); got != want {
			t.Errorf("%q: %v", text, got)
		}
	}
	if InfraSecretManifest("docs/a.yaml", "kind: Secret") {
		t.Fatal("outside k8s")
	}
	if _, bad := LandingContentViolation("bifrost-trade-infra", []LandingChange{{NewPath: "k8s/s.yaml", NewMode: "100644", AddedText: "kind: Secret\n"}}); !bad {
		t.Fatal("secret not refused")
	}
	if _, bad := LandingContentViolation("bifrost-platform", []LandingChange{{NewPath: "k8s/s.yaml", NewMode: "100644", AddedText: "kind: Secret\n"}}); bad {
		t.Fatal("platform content check")
	}
}

// T16 is an inner parser test: Git cannot create dot/traversal endpoints.
func TestInfraExistingExecutableScripts(t *testing.T) {
	t.Parallel()
	base := LandingChange{Kind: "M", OldPath: "scripts/agent-host/agentd-update", NewPath: "scripts/agent-host/agentd-update", OldMode: "100755", NewMode: "100755", Added: 1, Deleted: 1}
	cases := []struct {
		name    string
		edit    func(*LandingChange)
		allowed bool
	}{
		{"existing script content", func(*LandingChange) {}, true},
		{"existing installer", func(c *LandingChange) {
			c.OldPath = "scripts/k3s/apply-cicd-platform-pipeline.sh"
			c.NewPath = c.OldPath
		}, true},
		{"new executable", func(c *LandingChange) { c.Kind = "A"; c.OldPath = ""; c.OldMode = "" }, false},
		{"add execute permission", func(c *LandingChange) { c.OldMode = "100644" }, false},
		{"remove execute permission", func(c *LandingChange) { c.NewMode = "100644" }, false},
		{"rename executable", func(c *LandingChange) { c.Kind = "R"; c.NewPath = "scripts/other.sh" }, false},
		{"outside scripts", func(c *LandingChange) { c.OldPath = "config/executable"; c.NewPath = c.OldPath }, false},
		{"symlink", func(c *LandingChange) { c.Kind = "T"; c.NewMode = "120000" }, false},
		{"submodule", func(c *LandingChange) { c.Kind = "T"; c.NewMode = "160000" }, false},
		{"binary", func(c *LandingChange) { c.Binary = true }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := base
			tc.edit(&c)
			why, bad := LandingStructuralViolation("bifrost-trade-infra", []LandingChange{c})
			if bad == tc.allowed {
				t.Fatalf("allowed=%v: refusal=%q", tc.allowed, why)
			}
		})
	}
}

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
