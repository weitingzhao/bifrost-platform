package devagent

import (
	"strings"
	"testing"
)

func TestSkillFileLoaded(t *testing.T) {
	workspace := "/Users/vision-mac-trader/Desktop/stocks"
	if !skillFileLoaded(workspace, ".cursor/skills/ib-migration/SKILL.md") {
		t.Skip("ib-migration skill not present on this machine")
	}
	if skillFileLoaded(workspace, ".cursor/skills/does-not-exist/SKILL.md") {
		t.Fatal("expected missing skill to return false")
	}
}

func TestBridgeArgs(t *testing.T) {
	h := &Handler{
		repoRoot:  "/tmp/bifrost-platform",
		bridgeCmd: "node",
	}
	bp := &ProgramBlueprint{
		ID:        "test-program",
		Workspace: "/Users/vision-mac-trader/Desktop/stocks",
		SkillPath: ".cursor/skills/ib-migration/SKILL.md",
		Model:     "composer-2.5",
		Phases: []PhaseBlueprint{
			{
				ID:             "P0",
				Title:          "Phase 0",
				PromptTemplate: "Run {{phase_id}} for {{id}}",
			},
		},
	}

	args := h.bridgeArgs(bp, "P0")
	joined := strings.Join(args, " ")
	for _, needle := range []string{
		"--prompt", "Run P0 for test-program",
		"--phase", "P0",
		"--workspace", bp.Workspace,
		"--model", "composer-2.5",
		"--skill-path", bp.SkillPath,
		"/tmp/bifrost-platform/scripts/dev-agent/dist/bridge.js",
	} {
		if !strings.Contains(joined, needle) {
			t.Fatalf("bridge args missing %q in %q", needle, joined)
		}
	}
}
