package devagent

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func writeTestTemplates(t *testing.T, programsDir string) {
	t.Helper()
	tpl := `version: "1"
templates:
  - id: rocket-build
    title: Rocket Build
    description: test
    base_blueprint_id: control-room-ui
  - id: satellite-build
    title: Satellite Build
    description: test
    base_blueprint_id: trade-ib-client-migration
  - id: engineer-build
    title: Engineer Build
    description: test
    base_blueprint_id: dev-agent
  - id: ground-build
    title: Ground Build
    description: test
    base_blueprint_id: network-governance
  - id: plugin-build
    title: Plugin Build
    description: test
    base_blueprint_id: ib-gateway-plugin
`
	if err := os.WriteFile(filepath.Join(programsDir, "_templates.yaml"), []byte(tpl), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHandleCreateFromTemplate(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	programsDir := filepath.Join(configDir, "programs")
	if err := os.MkdirAll(programsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestTemplates(t, programsDir)
	_ = os.Setenv("PLATFORM_DATA_DIR", filepath.Join(dir, "data"))
	t.Cleanup(func() { _ = os.Unsetenv("PLATFORM_DATA_DIR") })

	base := &ProgramBlueprint{
		ID:          "control-room-ui",
		Title:       "Control Room UI",
		Description: "Test base blueprint",
		Status:      "active",
		Delivery: &DeliveryConfig{
			BoardVisible:     true,
			SignOffMechanism: "api",
		},
		Phases: []PhaseBlueprint{
			{ID: "P0", Title: "P0", Status: "pending"},
			{ID: "P1", Title: "P1", Status: "pending"},
		},
	}
	data, err := yaml.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	if writeErr := os.WriteFile(filepath.Join(programsDir, "control-room-ui.yaml"), data, 0o644); writeErr != nil {
		t.Fatal(writeErr)
	}

	h, err := NewHandler(configDir)
	if err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"template_id":"rocket-build","instance_label":"task-cc-test","notes":"unit test"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/programs/from-template", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleCreateFromTemplate(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var resp ProgramDetailBoardResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Program.ID != "control-room-ui--task-cc-test" {
		t.Fatalf("program id = %q", resp.Program.ID)
	}
	if len(resp.Phases) != 2 {
		t.Fatalf("phases = %d", len(resp.Phases))
	}

	// Idempotent when same instance label.
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/programs/from-template", bytes.NewReader(body))
	rec2 := httptest.NewRecorder()
	h.HandleCreateFromTemplate(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("idempotent status = %d body=%s", rec2.Code, rec2.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/programs?template_id=rocket-build", nil)
	listRec := httptest.NewRecorder()
	h.HandlePrograms(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d", listRec.Code)
	}
	var listResp struct {
		Programs []ProgramSummary `json:"programs"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	if len(listResp.Programs) != 1 {
		t.Fatalf("filtered programs = %d", len(listResp.Programs))
	}
}

func TestHandleCreateFromTemplateUnknown(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	programsDir := filepath.Join(configDir, "programs")
	if err := os.MkdirAll(programsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestTemplates(t, programsDir)
	_ = os.Setenv("PLATFORM_DATA_DIR", filepath.Join(dir, "data"))
	t.Cleanup(func() { _ = os.Unsetenv("PLATFORM_DATA_DIR") })

	baseYAML := `id: control-room-ui
title: Control Room UI
description: test
status: active
phases:
  - id: P0
    title: P0
    status: pending
`
	if err := os.WriteFile(filepath.Join(programsDir, "control-room-ui.yaml"), []byte(baseYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(configDir)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/programs/from-template", bytes.NewReader([]byte(`{"template_id":"missing"}`)))
	rec := httptest.NewRecorder()
	h.HandleCreateFromTemplate(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetProgramTemplate(t *testing.T) {
	// Load from repo config (ensures YAML is the authority).
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Clean(filepath.Join(wd, "..", "..", "..", "config"))
	if err := InitProgramTemplates(configDir); err != nil {
		t.Fatal(err)
	}
	tmpl, ok := GetProgramTemplate("satellite-build")
	if !ok {
		t.Fatal("expected satellite-build template")
	}
	if tmpl.BaseBlueprintID != "trade-ib-client-migration" {
		t.Fatalf("base = %q", tmpl.BaseBlueprintID)
	}

	cases := []struct {
		id   string
		base string
	}{
		{"engineer-build", "dev-agent"},
		{"ground-build", "network-governance"},
		{"plugin-build", "ib-gateway-plugin"},
	}
	for _, tc := range cases {
		got, ok := GetProgramTemplate(tc.id)
		if !ok {
			t.Fatalf("expected %s template", tc.id)
		}
		if got.BaseBlueprintID != tc.base {
			t.Fatalf("%s base = %q want %q", tc.id, got.BaseBlueprintID, tc.base)
		}
	}
}

func TestSlugInstanceLabel(t *testing.T) {
	if slugInstanceLabel("My Build #3") != "my-build-3" {
		t.Fatalf("slug = %q", slugInstanceLabel("My Build #3"))
	}
}
