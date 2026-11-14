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
