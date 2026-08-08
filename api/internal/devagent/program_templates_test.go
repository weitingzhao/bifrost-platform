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
  - id: build
    title: Build
    description: unified build
    base_blueprint_id: control-room-ui
    lane_blueprint_map:
      console-api: control-room-ui
      trade-stack: trade-ib-client-migration
      agent-infra: dev-agent
      network-server: network-governance
      agent-services: ib-gateway-plugin
`
	if err := os.WriteFile(filepath.Join(programsDir, "_templates.yaml"), []byte(tpl), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeMinimalBlueprint(t *testing.T, programsDir, id, title string) {
	t.Helper()
	base := &ProgramBlueprint{
		ID:          id,
		Title:       title,
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
	if err := os.WriteFile(filepath.Join(programsDir, id+".yaml"), data, 0o644); err != nil {
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

	writeMinimalBlueprint(t, programsDir, "control-room-ui", "Control Room UI")

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

func TestHandleCreateFromTemplateBuildLane(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	programsDir := filepath.Join(configDir, "programs")
	if err := os.MkdirAll(programsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestTemplates(t, programsDir)
	_ = os.Setenv("PLATFORM_DATA_DIR", filepath.Join(dir, "data"))
	t.Cleanup(func() { _ = os.Unsetenv("PLATFORM_DATA_DIR") })

	writeMinimalBlueprint(t, programsDir, "control-room-ui", "Control Room UI")
	writeMinimalBlueprint(t, programsDir, "trade-ib-client-migration", "Trade IB Client Migration")

	h, err := NewHandler(configDir)
	if err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"template_id":"build","instance_label":"lane-test","lane_id":"trade-stack","notes":"lane resolve"}`)
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
	if resp.Program.ID != "trade-ib-client-migration--lane-test" {
		t.Fatalf("program id = %q want trade-ib-client-migration--lane-test", resp.Program.ID)
	}
	if resp.Program.LaneID != "trade-stack" {
		t.Fatalf("lane_id = %q", resp.Program.LaneID)
	}

	// Verify written blueprint metadata used resolved base.
	raw, err := os.ReadFile(filepath.Join(programsDir, "trade-ib-client-migration--lane-test.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var written ProgramBlueprint
	if err := yaml.Unmarshal(raw, &written); err != nil {
		t.Fatal(err)
	}
	if written.Metadata["template_id"] != "build" {
		t.Fatalf("template_id = %v", written.Metadata["template_id"])
	}
	if written.Metadata["base_blueprint_id"] != "trade-ib-client-migration" {
		t.Fatalf("base_blueprint_id = %v", written.Metadata["base_blueprint_id"])
	}
	if written.Metadata["lane_id"] != "trade-stack" {
		t.Fatalf("lane_id = %v", written.Metadata["lane_id"])
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
		{"build", "control-room-ui"},
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

	build, ok := GetProgramTemplate("build")
	if !ok {
		t.Fatal("expected build template")
	}
	if build.ResolveBaseBlueprintID("trade-stack") != "trade-ib-client-migration" {
		t.Fatalf("lane resolve trade-stack = %q", build.ResolveBaseBlueprintID("trade-stack"))
	}
	if build.ResolveBaseBlueprintID("") != "control-room-ui" {
		t.Fatalf("default resolve = %q", build.ResolveBaseBlueprintID(""))
	}
	if build.ResolveBaseBlueprintID("unknown-lane") != "control-room-ui" {
		t.Fatalf("unknown lane fallback = %q", build.ResolveBaseBlueprintID("unknown-lane"))
	}
	if len(build.LaneBlueprintMap) < 5 {
		t.Fatalf("lane_blueprint_map size = %d", len(build.LaneBlueprintMap))
	}
}

func TestResolveBaseBlueprintID(t *testing.T) {
	tmpl := ProgramTemplate{
		ID:              "build",
		BaseBlueprintID: "control-room-ui",
		LaneBlueprintMap: map[string]string{
			"trade-stack": "trade-ib-client-migration",
		},
	}
	if got := tmpl.ResolveBaseBlueprintID("trade-stack"); got != "trade-ib-client-migration" {
		t.Fatalf("got %q", got)
	}
	if got := tmpl.ResolveBaseBlueprintID("  "); got != "control-room-ui" {
		t.Fatalf("blank lane got %q", got)
	}
}

func TestSlugInstanceLabel(t *testing.T) {
	if slugInstanceLabel("My Build #3") != "my-build-3" {
		t.Fatalf("slug = %q", slugInstanceLabel("My Build #3"))
	}
}

func signAllRuntimeGates(rt *programRuntime) {
	if rt.state == nil {
		rt.state = &ProgramStateRecord{ProgramID: rt.blueprint.ID}
	}
	rt.state.PhaseSignOffs = nil
	for _, p := range rt.blueprint.Phases {
		rt.state.PhaseSignOffs = append(rt.state.PhaseSignOffs, PhaseSignOffRecord{
			PhaseID:     p.ID,
			SignedOffAt: "2026-08-08T00:00:00Z",
			SignedOffBy: "test",
		})
	}
}

func TestHandleCreateFromTemplateRejectsSecondLiveLaneBind(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	programsDir := filepath.Join(configDir, "programs")
	if err := os.MkdirAll(programsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestTemplates(t, programsDir)
	_ = os.Setenv("PLATFORM_DATA_DIR", filepath.Join(dir, "data"))
	t.Cleanup(func() { _ = os.Unsetenv("PLATFORM_DATA_DIR") })
	writeMinimalBlueprint(t, programsDir, "control-room-ui", "Control Room UI")

	h, err := NewHandler(configDir)
	if err != nil {
		t.Fatal(err)
	}

	body1 := []byte(`{"template_id":"build","instance_label":"first","lane_id":"console-api"}`)
	rec1 := httptest.NewRecorder()
	h.HandleCreateFromTemplate(rec1, httptest.NewRequest(http.MethodPost, "/api/v1/programs/from-template", bytes.NewReader(body1)))
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first create status = %d body=%s", rec1.Code, rec1.Body.String())
	}

	body2 := []byte(`{"template_id":"build","instance_label":"second","lane_id":"console-api"}`)
	rec2 := httptest.NewRecorder()
	h.HandleCreateFromTemplate(rec2, httptest.NewRequest(http.MethodPost, "/api/v1/programs/from-template", bytes.NewReader(body2)))
	if rec2.Code != http.StatusConflict {
		t.Fatalf("second live bind status = %d body=%s", rec2.Code, rec2.Body.String())
	}
	if !bytes.Contains(rec2.Body.Bytes(), []byte("control-room-ui--first")) {
		t.Fatalf("409 body should name blocking program: %s", rec2.Body.String())
	}
}

func TestHandleCreateFromTemplateAllowsBindWhenSiblingSessionReleased(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	programsDir := filepath.Join(configDir, "programs")
	if err := os.MkdirAll(programsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestTemplates(t, programsDir)
	_ = os.Setenv("PLATFORM_DATA_DIR", filepath.Join(dir, "data"))
	t.Cleanup(func() { _ = os.Unsetenv("PLATFORM_DATA_DIR") })
	writeMinimalBlueprint(t, programsDir, "control-room-ui", "Control Room UI")

	h, err := NewHandler(configDir)
	if err != nil {
		t.Fatal(err)
	}

	body1 := []byte(`{"template_id":"build","instance_label":"closed","lane_id":"console-api"}`)
	rec1 := httptest.NewRecorder()
	h.HandleCreateFromTemplate(rec1, httptest.NewRequest(http.MethodPost, "/api/v1/programs/from-template", bytes.NewReader(body1)))
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first create status = %d body=%s", rec1.Code, rec1.Body.String())
	}

	h.mu.Lock()
	rt := h.runtimes["control-room-ui--closed"]
	signAllRuntimeGates(rt)
	h.mu.Unlock()

	body2 := []byte(`{"template_id":"build","instance_label":"next","lane_id":"console-api"}`)
	rec2 := httptest.NewRecorder()
	h.HandleCreateFromTemplate(rec2, httptest.NewRequest(http.MethodPost, "/api/v1/programs/from-template", bytes.NewReader(body2)))
	if rec2.Code != http.StatusCreated {
		t.Fatalf("bind after sessionReleased status = %d body=%s", rec2.Code, rec2.Body.String())
	}
}

func TestHandleCreateFromTemplateAllowsBindWhenSiblingInOperate(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	programsDir := filepath.Join(configDir, "programs")
	if err := os.MkdirAll(programsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestTemplates(t, programsDir)
	_ = os.Setenv("PLATFORM_DATA_DIR", filepath.Join(dir, "data"))
	t.Cleanup(func() { _ = os.Unsetenv("PLATFORM_DATA_DIR") })
	writeMinimalBlueprint(t, programsDir, "control-room-ui", "Control Room UI")

	h, err := NewHandler(configDir)
	if err != nil {
		t.Fatal(err)
	}

	body1 := []byte(`{"template_id":"build","instance_label":"ops","lane_id":"console-api"}`)
	rec1 := httptest.NewRecorder()
	h.HandleCreateFromTemplate(rec1, httptest.NewRequest(http.MethodPost, "/api/v1/programs/from-template", bytes.NewReader(body1)))
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first create status = %d body=%s", rec1.Code, rec1.Body.String())
	}

	h.mu.Lock()
	rt := h.runtimes["control-room-ui--ops"]
	signAllRuntimeGates(rt)
	rt.blueprint.PostCompletion = &PostCompletionBlueprint{NewCapabilities: []string{"x"}}
	rt.state.PostCompletion = &PostCompletionState{AssessmentStatus: "in_operate"}
	h.mu.Unlock()

	body2 := []byte(`{"template_id":"build","instance_label":"after-ops","lane_id":"console-api"}`)
	rec2 := httptest.NewRecorder()
	h.HandleCreateFromTemplate(rec2, httptest.NewRequest(http.MethodPost, "/api/v1/programs/from-template", bytes.NewReader(body2)))
	if rec2.Code != http.StatusCreated {
		t.Fatalf("bind after in_operate status = %d body=%s", rec2.Code, rec2.Body.String())
	}
}
