package devagent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ProgramBlueprint is the declarative program definition loaded from config/programs/*.yaml.
type ProgramBlueprint struct {
	ID          string                 `yaml:"id" json:"id"`
	Title       string                 `yaml:"title" json:"title"`
	Description string                 `yaml:"description" json:"description"`
	Status      string                 `yaml:"status" json:"status"`
	Workspace   string                 `yaml:"workspace" json:"workspace"`
	SkillPath   string                 `yaml:"skill_path" json:"skill_path"`
	Model       string                 `yaml:"model" json:"model"`
	Phases      []PhaseBlueprint       `yaml:"phases" json:"phases"`
	Metadata    map[string]interface{} `yaml:"metadata" json:"metadata,omitempty"`
}

type PhaseBlueprint struct {
	ID             string   `yaml:"id" json:"id"`
	Title          string   `yaml:"title" json:"title"`
	Status         string   `yaml:"status" json:"status"`
	PromptTemplate string   `yaml:"prompt_template" json:"prompt_template,omitempty"`
	VerifyCmd      string   `yaml:"verify_cmd" json:"verify_cmd,omitempty"`
	Acceptance     []string `yaml:"acceptance" json:"acceptance,omitempty"`
	DependsOn      []string `yaml:"depends_on" json:"depends_on,omitempty"`
}

type ProgramSummary struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	Status        string `json:"status"`
	PhaseCount    int    `json:"phase_count"`
	PhasesDone    int    `json:"phases_done"`
	AllPhasesDone bool   `json:"all_phases_done"`
	Active        bool   `json:"active"`
}

type ProgramInfo struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

func LoadProgramBlueprints(dir string) ([]*ProgramBlueprint, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read programs dir %s: %w", dir, err)
	}

	var programs []*ProgramBlueprint
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		if strings.HasPrefix(name, "_") || name == "example-template.yaml" {
			continue
		}

		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}

		var bp ProgramBlueprint
		if err := yaml.Unmarshal(data, &bp); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		if bp.ID == "" {
			return nil, fmt.Errorf("program in %s missing id", path)
		}
		if bp.Model == "" {
			bp.Model = "composer-2.5"
		}
		programs = append(programs, &bp)
	}

	if len(programs) == 0 {
		return nil, fmt.Errorf("no program blueprints found in %s", dir)
	}
	return programs, nil
}

func phasesFromBlueprint(bp *ProgramBlueprint) []Phase {
	phases := make([]Phase, len(bp.Phases))
	for i, p := range bp.Phases {
		phases[i] = Phase{
			ID:     p.ID,
			Title:  p.Title,
			Status: parsePhaseStatus(p.Status),
		}
	}
	return phases
}

func parsePhaseStatus(s string) PhaseStatus {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "done":
		return PhaseDone
	case "running":
		return PhaseRunning
	case "failed":
		return PhaseFailed
	default:
		return PhasePending
	}
}

func renderPrompt(tmpl string, vars map[string]string) string {
	out := tmpl
	for k, v := range vars {
		out = strings.ReplaceAll(out, "{{"+k+"}}", v)
	}
	return out
}

func resolveSkillPath(workspace, skillPath string) string {
	if skillPath == "" {
		return ""
	}
	if filepath.IsAbs(skillPath) {
		return skillPath
	}
	return filepath.Join(workspace, skillPath)
}

func skillFileLoaded(workspace, skillPath string) bool {
	if strings.TrimSpace(skillPath) == "" {
		return false
	}
	path := resolveSkillPath(workspace, skillPath)
	_, err := os.Stat(path)
	return err == nil
}

func promptForPhase(bp *ProgramBlueprint, phaseID string) string {
	for _, p := range bp.Phases {
		if p.ID != phaseID {
			continue
		}
		if strings.TrimSpace(p.PromptTemplate) != "" {
			return renderPrompt(p.PromptTemplate, map[string]string{
				"id":           bp.ID,
				"skill_path":   bp.SkillPath,
				"workspace":    bp.Workspace,
				"phase_id":     p.ID,
				"phase_title":  p.Title,
				"verify_cmd":   p.VerifyCmd,
			})
		}
		break
	}
	return fmt.Sprintf(
		"Execute %s phase %s for program %s. Follow %s. Output a structured Phase completion report at the end.",
		bp.Title, phaseID, bp.ID, bp.SkillPath,
	)
}
