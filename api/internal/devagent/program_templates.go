package devagent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// ProgramTemplate defines how to spawn a Delivery Board program instance for a dev task mode.
type ProgramTemplate struct {
	ID               string            `yaml:"id" json:"id"`
	Title            string            `yaml:"title" json:"title"`
	Description      string            `yaml:"description" json:"description"`
	BaseBlueprintID  string            `yaml:"base_blueprint_id" json:"base_blueprint_id"`
	LaneBlueprintMap map[string]string `yaml:"lane_blueprint_map,omitempty" json:"lane_blueprint_map,omitempty"`
}

// ResolveBaseBlueprintID returns the blueprint id for a spawn: lane map override when present, else BaseBlueprintID.
func (t ProgramTemplate) ResolveBaseBlueprintID(laneID string) string {
	lane := strings.TrimSpace(laneID)
	if lane != "" && t.LaneBlueprintMap != nil {
		if mapped, ok := t.LaneBlueprintMap[lane]; ok && strings.TrimSpace(mapped) != "" {
			return strings.TrimSpace(mapped)
		}
	}
	return strings.TrimSpace(t.BaseBlueprintID)
}

type templatesFile struct {
	Version   string            `yaml:"version"`
	Templates []ProgramTemplate `yaml:"templates"`
}

var (
	templatesMu    sync.RWMutex
	templatesByID  map[string]ProgramTemplate
	templatesPath  string
	templatesLoaded bool
)

// InitProgramTemplates loads config/programs/_templates.yaml (called from NewHandler).
func InitProgramTemplates(configDir string) error {
	path := filepath.Join(configDir, "programs", "_templates.yaml")
	return loadProgramTemplates(path)
}

func loadProgramTemplates(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read program templates: %w", err)
	}
	var file templatesFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("parse program templates: %w", err)
	}
	if len(file.Templates) == 0 {
		return fmt.Errorf("no templates in %s", path)
	}
	m := make(map[string]ProgramTemplate, len(file.Templates))
	for _, t := range file.Templates {
		id := strings.TrimSpace(t.ID)
		if id == "" || strings.TrimSpace(t.BaseBlueprintID) == "" {
			return fmt.Errorf("template missing id or base_blueprint_id in %s", path)
		}
		t.ID = id
		m[id] = t
	}
	templatesMu.Lock()
	templatesByID = m
	templatesPath = path
	templatesLoaded = true
	templatesMu.Unlock()
	return nil
}

func ensureTemplatesLoaded() error {
	templatesMu.RLock()
	ok := templatesLoaded
	templatesMu.RUnlock()
	if ok {
		return nil
	}
	// Fallback for tests / early calls: resolve repo config relative to cwd.
	candidates := []string{
		filepath.Join("config", "programs", "_templates.yaml"),
		filepath.Join("..", "config", "programs", "_templates.yaml"),
		filepath.Join("..", "..", "..", "config", "programs", "_templates.yaml"),
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, "config", "programs", "_templates.yaml"),
			filepath.Join(wd, "..", "config", "programs", "_templates.yaml"),
			filepath.Join(wd, "..", "..", "..", "config", "programs", "_templates.yaml"),
		)
	}
	var last error
	for _, p := range candidates {
		if _, err := os.Stat(p); err != nil {
			last = err
			continue
		}
		return loadProgramTemplates(p)
	}
	if last != nil {
		return fmt.Errorf("program templates not loaded: %w", last)
	}
	return fmt.Errorf("program templates not loaded")
}

func GetProgramTemplate(templateID string) (ProgramTemplate, bool) {
	if err := ensureTemplatesLoaded(); err != nil {
		return ProgramTemplate{}, false
	}
	templatesMu.RLock()
	defer templatesMu.RUnlock()
	t, ok := templatesByID[strings.TrimSpace(templateID)]
	return t, ok
}

func ListProgramTemplates() []ProgramTemplate {
	if err := ensureTemplatesLoaded(); err != nil {
		return nil
	}
	templatesMu.RLock()
	defer templatesMu.RUnlock()
	out := make([]ProgramTemplate, 0, len(templatesByID))
	for _, t := range templatesByID {
		out = append(out, t)
	}
	return out
}

func (h *Handler) HandleListTemplates(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"templates": ListProgramTemplates(),
		"path":      templatesPath,
	})
}

type CreateFromTemplateRequest struct {
	TemplateID    string `json:"template_id"`
	InstanceLabel string `json:"instance_label,omitempty"`
	Notes         string `json:"notes,omitempty"`
	LaneID        string `json:"lane_id,omitempty"`
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

func cloneBlueprintInstance(base *ProgramBlueprint, tmpl ProgramTemplate, programID, instanceLabel, notes, laneID string) *ProgramBlueprint {
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
		meta := make(map[string]interface{}, len(clone.Metadata)+5)
		for k, v := range clone.Metadata {
			meta[k] = v
		}
		clone.Metadata = meta
	} else {
		clone.Metadata = map[string]interface{}{}
	}
	clone.Metadata["template_id"] = tmpl.ID
	clone.Metadata["base_blueprint_id"] = tmpl.ResolveBaseBlueprintID(laneID)
	if strings.TrimSpace(instanceLabel) != "" {
		clone.Metadata["instance_label"] = strings.TrimSpace(instanceLabel)
	}
	if strings.TrimSpace(notes) != "" {
		clone.Metadata["notes"] = strings.TrimSpace(notes)
	}
	if strings.TrimSpace(laneID) != "" {
		clone.Metadata["lane_id"] = strings.TrimSpace(laneID)
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
	laneID := ""
	if bp.Metadata != nil {
		if v, ok := bp.Metadata["lane_id"].(string); ok {
			laneID = strings.TrimSpace(v)
		}
	}
	rt := &programRuntime{
		blueprint: bp,
		phases:    phasesFromBlueprint(bp),
		history:   []Job{},
		state: &ProgramStateRecord{
			ProgramID: bp.ID,
			LaneID:    laneID,
			History:   []Job{},
		},
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

	baseID := tmpl.ResolveBaseBlueprintID(req.LaneID)
	base, err := h.loadBaseBlueprint(baseID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	programID := instanceProgramID(baseID, req.InstanceLabel)

	h.mu.Lock()
	if _, exists := h.runtimes[programID]; exists {
		rt := h.runtimes[programID]
		h.mu.Unlock()
		writeJSON(w, http.StatusOK, h.programDetailBoardResponse(programID, rt))
		return
	}
	h.mu.Unlock()

	bp := cloneBlueprintInstance(base, tmpl, programID, req.InstanceLabel, req.Notes, req.LaneID)
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
