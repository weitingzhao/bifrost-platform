package store

import (
	"errors"
	"strings"
	"testing"
)

// A proposal's core label (the API's and a plan's, Owner 「核心字段 A」):
// core is an implementation in a repository with core work; on another role
// or in a repository with none it is refused.
func TestBatchHoldsCoreToImplementations(t *testing.T) {
	t.Parallel()
	task := func(role, repo string) TaskProposal {
		return TaskProposal{Ref: "t1", OffPlanReason: "x", Role: role, Class: "go", Title: "t", SpecArtifact: "sha256:x",
			Effect: "branch", Repo: repo, BaseCommit: "2333e00753f91c74c8b2f4d693a803f8e89e7892", Core: true}
	}
	if err := checkBatch([]TaskProposal{task("implement", "bifrost-platform")}); err != nil {
		t.Fatalf("a core implementation in the platform: %v", err)
	}
	for name, c := range map[string]struct {
		t   TaskProposal
		why string
	}{
		"a brief":     {task("brief", "bifrost-platform"), "a brief task is not core"},
		"a plan":      {task("plan", "bifrost-platform"), "a plan task is not core"},
		"infra":       {task("implement", "bifrost-trade-infra"), "bifrost-trade-infra has no core work"},
		"no repo":     {task("implement", ""), "a task with no repository has no core work"},
		"another one": {task("implement", "bifrost-research"), "bifrost-research has no core work"},
	} {
		err := checkBatch([]TaskProposal{c.t})
		if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), c.why) {
			t.Errorf("%s: %v (want %q)", name, err, c.why)
		}
	}
	plain := task("brief", "")
	plain.Core, plain.Effect = false, "none" // a brief pushes nothing: no repo, no checkout (TD-421)
	if err := checkBatch([]TaskProposal{plain}); err != nil {
		t.Fatalf("a brief that is not core: %v", err)
	}
}

// The planner's and the chief of staff's specs say what core work is, the
// same words (CoreText) and directories routing and the plan's check use;
// a planner is told how to mark it, and a goal with no core work says so.
func TestSpecsSayWhatCoreWorkIs(t *testing.T) {
	t.Parallel()
	text := func(b []byte, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	core := strings.ReplaceAll(CoreText(), "\n", `\n`) // as canonical JSON writes the text
	for _, dir := range []string{"api/internal/agentrt/", "api/internal/agentd/", "api/cmd/agentd/", "Codex reviews it"} {
		if !strings.Contains(core, dir) {
			t.Fatalf("CoreText lacks %q: %s", dir, core)
		}
	}
	bases := map[string]string{"bifrost-platform": "abc", "bifrost-trade-infra": "def"}
	plan := text(planSpec(Goal{Title: "a goal", Text: "do it"}, []string{"bifrost-platform", "bifrost-trade-infra"}, bases, "M-1", "Taken now."))
	brief := text(briefSpec("owner", "ask", "status?", nil, "Taken now."))
	for name, spec := range map[string]string{"plan": plan, "brief": brief} {
		if !strings.Contains(spec, core) {
			t.Errorf("the %s spec does not say what core work is", name)
		}
	}
	for _, want := range []string{`Mark a task \"core\": true when it is core work`, `\"core\": false}`,
		"in a\\n  repository with no core work (bifrost-trade-infra)", "as a path from the repository's root",
		"core is true or false (left out: false)"} {
		if !strings.Contains(plan, want) {
			t.Errorf("the plan spec lacks %q", want)
		}
	}
	if !strings.Contains(brief, "whether its work is\\ncore work") {
		t.Error("the chief of staff is not asked whether a goal it suggests is core work")
	}
	infra := text(planSpec(Goal{Title: "a goal", Text: "do it"}, []string{"bifrost-trade-infra"}, bases, "M-1", "Taken now."))
	if !strings.Contains(infra, "No task of this goal is core work") || strings.Contains(infra, `Mark a task \"core\"`) {
		t.Error("a goal in a repository with no core work is not told so")
	}
}
