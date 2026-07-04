package devagent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProgramBlueprints(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "config", "programs")
	if _, err := os.Stat(dir); err != nil {
		t.Skip("config/programs not found from test cwd")
	}

	programs, err := LoadProgramBlueprints(dir)
	if err != nil {
		t.Fatalf("LoadProgramBlueprints: %v", err)
	}
	if len(programs) == 0 {
		t.Fatal("expected at least one program")
	}

	var tibm *ProgramBlueprint
	for _, p := range programs {
		if p.ID == "trade-ib-client-migration" {
			tibm = p
			break
		}
	}
	if tibm == nil {
		t.Fatal("trade-ib-client-migration not loaded")
	}
	if len(tibm.Phases) != 5 {
		t.Fatalf("expected 5 phases, got %d", len(tibm.Phases))
	}
	if tibm.Model != "composer-2.5" {
		t.Fatalf("expected default model composer-2.5, got %q", tibm.Model)
	}
}

func TestRenderPrompt(t *testing.T) {
	tmpl := "Execute {{phase_id}}. Follow {{skill_path}}. Verify: {{verify_cmd}}"
	got := renderPrompt(tmpl, map[string]string{
		"phase_id":   "TIBM4",
		"skill_path": ".cursor/skills/ib-migration/SKILL.md",
		"verify_cmd": "make verify",
	})
	if !strings.Contains(got, "TIBM4") {
		t.Fatalf("missing phase_id: %q", got)
	}
	if !strings.Contains(got, ".cursor/skills/ib-migration/SKILL.md") {
		t.Fatalf("missing skill_path: %q", got)
	}
	if !strings.Contains(got, "make verify") {
		t.Fatalf("missing verify_cmd: %q", got)
	}
}

func TestPromptForPhase(t *testing.T) {
	bp := &ProgramBlueprint{
		ID:        "test-program",
		Title:     "Test",
		SkillPath: ".cursor/skills/test/SKILL.md",
		Phases: []PhaseBlueprint{
			{
				ID:    "P0",
				Title: "Phase 0",
				PromptTemplate: "Run {{phase_id}} for {{id}} using {{skill_path}}",
			},
		},
	}
	got := promptForPhase(bp, "P0")
	if !strings.Contains(got, "P0") || !strings.Contains(got, "test-program") {
		t.Fatalf("unexpected prompt: %q", got)
	}
}

func TestNewHandler(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "config")
	if _, err := os.Stat(filepath.Join(dir, "programs")); err != nil {
		t.Skip("config/programs not found from test cwd")
	}

	h, err := NewHandler(dir)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	if h.activeProgramID == "" {
		t.Fatal("expected an active program")
	}
	rt := h.activeRuntime()
	if rt == nil {
		t.Fatalf("expected active runtime for %q", h.activeProgramID)
	}
	if len(rt.phases) == 0 {
		t.Fatalf("expected phases for active program %q", h.activeProgramID)
	}
}
