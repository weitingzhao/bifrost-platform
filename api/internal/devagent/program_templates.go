package devagent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ProgramTemplate defines how to spawn a Delivery Board program instance for a dev task mode.
type ProgramTemplate struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Description     string `json:"description"`
	BaseBlueprintID string `json:"base_blueprint_id"`
}

var programTemplates = map[string]ProgramTemplate{
	"rocket-build": {
		ID:              "rocket-build",
		Title:           "Rocket Build",
		Description:     "Dev loop instance for platform Console/API work.",
		BaseBlueprintID: "control-room-ui",
	},
	"satellite-build": {
		ID:              "satellite-build",
		Title:           "Satellite Build",
		Description:     "Dev loop instance for trade stack migration/build work.",
		BaseBlueprintID: "trade-ib-client-migration",
	},
	"engineer-build": {
		ID:              "engineer-build",
		Title:           "Engineer Build",
		Description:     "Dev loop instance for agent infra / Dev Agent platform work.",
		BaseBlueprintID: "dev-agent",
	},
	"ground-build": {
		ID:              "ground-build",
		Title:           "Ground Build",
		Description:     "Dev loop instance for ground systems / network governance work.",
		BaseBlueprintID: "network-governance",
	},
	"plugin-build": {
		ID:              "plugin-build",
		Title:           "Plugin Build",
		Description:     "Dev loop instance for platform plugin work (e.g. IB Gateway).",
		BaseBlueprintID: "ib-gateway-plugin",
	},
}

func GetProgramTemplate(templateID string) (ProgramTemplate, bool) {
	t, ok := programTemplates[strings.TrimSpace(templateID)]
	return t, ok
}

func ListProgramTemplates() []ProgramTemplate {
	out := make([]ProgramTemplate, 0, len(programTemplates))
	for _, t := range programTemplates {
		out = append(out, t)
	}
	return out
}

type CreateFromTemplateRequest struct {
	TemplateID    string `json:"template_id"`
	InstanceLabel string `json:"instance_label,omitempty"`
	Notes         string `json:"notes,omitempty"`
}

var instanceSlugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugInstanceLabel(label string) string {
	s := strings.ToLower(strings.TrimSpace(label))
	s = instanceSlugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 48 {
		s = s[:48]
	}
	if s == "" {
		return "instance"
	}
	return s
}

func instanceProgramID(baseID, instanceLabel string) string {
	if strings.TrimSpace(instanceLabel) != "" {
		return fmt.Sprintf("%s--%s", baseID, slugInstanceLabel(instanceLabel))
	}
	return fmt.Sprintf("%s--%s", baseID, time.Now().UTC().Format("20060102150405"))
}

func cloneBlueprintInstance(base *ProgramBlueprint, tmpl ProgramTemplate, programID, instanceLabel, notes string) *ProgramBlueprint {
	clone := *base
	clone.ID = programID
	clone.Title = base.Title
	if strings.TrimSpace(instanceLabel) != "" {
		clone.Title = fmt.Sprintf("%s · %s", base.Title, instanceLabel)
	}
	if clone.Delivery != nil {
		d := *clone.Delivery
		d.BoardVisible = true
		if d.SignOffMechanism == "" {
			d.SignOffMechanism = "api"
		}
		clone.Delivery = &d
	} else {
		clone.Delivery = &DeliveryConfig{
			BoardVisible:     true,
			SignOffMechanism: "api",
		}
	}
	if clone.Metadata != nil {
		meta := make(map[string]interface{}, len(clone.Metadata)+4)
		for k, v := range clone.Metadata {
			meta[k] = v
		}
		clone.Metadata = meta
	} else {
		clone.Metadata = map[string]interface{}{}
	}
	clone.Metadata["template_id"] = tmpl.ID
	clone.Metadata["base_blueprint_id"] = tmpl.BaseBlueprintID
	if strings.TrimSpace(instanceLabel) != "" {
		clone.Metadata["instance_label"] = strings.TrimSpace(instanceLabel)
	}
	if strings.TrimSpace(notes) != "" {
		clone.Metadata["notes"] = strings.TrimSpace(notes)
	}
	clone.Metadata["created_at"] = time.Now().UTC().Format(time.RFC3339)
	return &clone
}

func writeProgramBlueprint(dir string, bp *ProgramBlueprint) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir programs config: %w", err)
	}
	data, err := yaml.Marshal(bp)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, bp.ID+".yaml")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write program blueprint: %w", err)
	}
	return os.Rename(tmp, path)
}

func templateIDFromRuntime(rt *programRuntime) string {
	if rt == nil || rt.blueprint == nil || rt.blueprint.Metadata == nil {
		return ""
	}
	if v, ok := rt.blueprint.Metadata["template_id"].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func (h *Handler) loadBaseBlueprint(baseID string) (*ProgramBlueprint, error) {
	h.mu.Lock()
	if rt, ok := h.runtimes[baseID]; ok {
		bp := rt.blueprint
		h.mu.Unlock()
		return bp, nil
	}
	h.mu.Unlock()

	path := filepath.Join(h.blueprintDir, baseID+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("base blueprint not found: %s", baseID)
		}
		return nil, fmt.Errorf("read base blueprint %s: %w", baseID, err)
	}
	var bp ProgramBlueprint
	if err := yaml.Unmarshal(data, &bp); err != nil {
		return nil, fmt.Errorf("parse base blueprint %s: %w", baseID, err)
	}
	if bp.ID == "" {
		bp.ID = baseID
	}
	return &bp, nil
}

func (h *Handler) registerProgramRuntime(bp *ProgramBlueprint) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.runtimes[bp.ID]; exists {
		return fmt.Errorf("program already exists: %s", bp.ID)
	}
	rt := &programRuntime{
		blueprint: bp,
		phases:    phasesFromBlueprint(bp),
		history:   []Job{},
		state:     &ProgramStateRecord{ProgramID: bp.ID, History: []Job{}},
	}
	h.runtimes[bp.ID] = rt
	return h.persistRuntimeLocked(bp.ID)
}

func (h *Handler) HandleCreateFromTemplate(w http.ResponseWriter, r *http.Request) {
	var req CreateFromTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	templateID := strings.TrimSpace(req.TemplateID)
	if templateID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "template_id required"})
		return
	}
	tmpl, ok := GetProgramTemplate(templateID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
		return
	}

	base, err := h.loadBaseBlueprint(tmpl.BaseBlueprintID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	programID := instanceProgramID(tmpl.BaseBlueprintID, req.InstanceLabel)

	h.mu.Lock()
	if _, exists := h.runtimes[programID]; exists {
		rt := h.runtimes[programID]
		h.mu.Unlock()
		writeJSON(w, http.StatusOK, h.programDetailBoardResponse(programID, rt))
		return
	}
	h.mu.Unlock()

	bp := cloneBlueprintInstance(base, tmpl, programID, req.InstanceLabel, req.Notes)
	if err := writeProgramBlueprint(h.blueprintDir, bp); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := h.registerProgramRuntime(bp); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.mu.Lock()
	rt := h.runtimes[programID]
	h.mu.Unlock()
	writeJSON(w, http.StatusCreated, h.programDetailBoardResponse(programID, rt))
}
