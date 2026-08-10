package devagent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemapPhaseIDs(t *testing.T) {
	aliases := map[string]string{
		"TIBM0": "P1",
		"TIBM1": "P2",
	}
	state := &ProgramStateRecord{
		ProgramID: "trade-ib-client-migration",
		Phases:    []Phase{{ID: "TIBM0", Status: PhaseDone}, {ID: "TIBM1", Status: PhasePending}},
		ActiveJob: &Job{ID: "j1", PhaseID: "TIBM1", Status: JobRunning},
		History:   []Job{{ID: "j0", PhaseID: "TIBM0", Status: JobDone}},
		PhaseSignOffs: []PhaseSignOffRecord{
			{PhaseID: "TIBM0", SignedOffAt: "2026-07-01T00:00:00Z"},
		},
		PhaseProgress: []PhaseProgressRecord{
			{PhaseID: "TIBM0", Status: "done"},
		},
		AgentSessions: []AgentSessionRecord{
			{ID: "s1", PhaseID: "TIBM1"},
		},
	}
	if !RemapPhaseIDs(state, aliases) {
		t.Fatal("expected remap to change state")
	}
	if state.Phases[0].ID != "P1" || state.Phases[1].ID != "P2" {
		t.Fatalf("phases=%+v", state.Phases)
	}
	if state.ActiveJob.PhaseID != "P2" {
		t.Fatalf("active job phase=%s", state.ActiveJob.PhaseID)
	}
	if state.History[0].PhaseID != "P1" {
		t.Fatalf("history phase=%s", state.History[0].PhaseID)
	}
	if state.PhaseSignOffs[0].PhaseID != "P1" || state.PhaseProgress[0].PhaseID != "P1" {
		t.Fatalf("signoff/progress not remapped")
	}
	if state.AgentSessions[0].PhaseID != "P2" {
		t.Fatalf("session phase=%s", state.AgentSessions[0].PhaseID)
	}
	if RemapPhaseIDs(state, aliases) {
		t.Fatal("second remap should be a no-op")
	}
}

func TestCollectNamingWarningsActiveOnly(t *testing.T) {
	warnings := CollectNamingWarnings([]*ProgramBlueprint{
		{
			ID:     "vision",
			Status: "active",
			Phases: []PhaseBlueprint{{ID: "V1"}, {ID: "S3"}},
		},
		{
			ID:     "dap-smoke-test",
			Status: "active",
			Phases: []PhaseBlueprint{{ID: "SMOKE-0"}, {ID: "P1"}},
		},
		{
			ID:     "old-completed",
			Status: "completed",
			Phases: []PhaseBlueprint{{ID: "TIBM0"}},
		},
		{
			ID:     "governance",
			Status: "active",
			Phases: []PhaseBlueprint{{ID: "P1"}},
		},
		{
			ID:     "network-governance",
			Status: "active",
			Phases: []PhaseBlueprint{{ID: "NG1"}, {ID: "NG8"}},
		},
	})
	var smoke, gov, completed, netGov bool
	for _, w := range warnings {
		if w.ProgramID == "dap-smoke-test" && w.Field == "phase_id" {
			smoke = true
		}
		if w.ProgramID == "governance" && w.Field == "id" {
			gov = true
		}
		if w.ProgramID == "old-completed" {
			completed = true
		}
		if w.ProgramID == "vision" && w.Field == "phase_id" {
			t.Fatalf("vision legacy phases should not warn: %+v", w)
		}
		if w.ProgramID == "network-governance" && w.Field == "phase_id" {
			netGov = true
		}
	}
	if !smoke {
		t.Fatalf("expected SMOKE-0 warning, got %+v", warnings)
	}
	if !gov {
		t.Fatalf("expected governance id warning, got %+v", warnings)
	}
	if completed {
		t.Fatal("completed programs must not emit naming warnings")
	}
	if netGov {
		t.Fatal("network-governance NG1-NG8 should be exempt from phase_id warnings")
	}
}

func TestLoadProgramBlueprintsRecursiveAndExampleSkip(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "active")
	archived := filepath.Join(dir, "archived")
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(archived, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(active, "good-program.yaml"), `
id: good-program
title: Good
status: active
phases:
  - id: P1
    title: One
`)
	write(filepath.Join(archived, "old-shell.yaml"), `
id: old-shell
title: Old
status: archived
phases:
  - id: P1
    title: One
`)
	write(filepath.Join(dir, "_schema.yaml"), "id: not-a-program\n")
	write(filepath.Join(dir, "example-template.yaml"), "id: example-skip\nstatus: active\nphases: []\n")
	write(filepath.Join(dir, "example-other.yaml"), "id: example-other\nstatus: active\nphases: []\n")

	programs, err := LoadProgramBlueprints(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(programs) != 2 {
		t.Fatalf("got %d programs, want 2", len(programs))
	}
	ids := map[string]bool{}
	for _, p := range programs {
		ids[p.ID] = true
		if p.SourcePath == "" {
			t.Fatalf("missing SourcePath for %s", p.ID)
		}
	}
	if !ids["good-program"] || !ids["old-shell"] {
		t.Fatalf("ids=%v", ids)
	}
	if ids["example-skip"] || ids["example-other"] {
		t.Fatal("example-* must be skipped")
	}
}

func TestLoadProgramBlueprintsSkipsConfigMapAtomicDirs(t *testing.T) {
	dir := t.TempDir()
	atomic := filepath.Join(dir, "..2026_08_10_18_26_04.1331625417")
	if err := os.MkdirAll(atomic, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `
id: dap-smoke-test
title: Smoke
status: archived
phases:
  - id: P1
    title: One
`
	if err := os.WriteFile(filepath.Join(atomic, "dap-smoke-test.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// Root entry is the live ConfigMap projection (symlink to the atomic dir file).
	if err := os.Symlink(
		filepath.Join(atomic, "dap-smoke-test.yaml"),
		filepath.Join(dir, "dap-smoke-test.yaml"),
	); err != nil {
		t.Fatal(err)
	}

	programs, err := LoadProgramBlueprints(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(programs) != 1 || programs[0].ID != "dap-smoke-test" {
		t.Fatalf("got %+v", programs)
	}
}

func TestHandleProgramsArchivedFilter(t *testing.T) {
	h := &Handler{
		runtimes: map[string]*programRuntime{
			"live": {
				blueprint: &ProgramBlueprint{
					ID: "live", Title: "Live", Status: "active",
					Delivery: &DeliveryConfig{BoardVisible: true},
				},
			},
			"done": {
				blueprint: &ProgramBlueprint{
					ID: "done", Title: "Done", Status: "completed",
					Delivery: &DeliveryConfig{BoardVisible: true},
				},
			},
			"old": {
				blueprint: &ProgramBlueprint{
					ID: "old", Title: "Old", Status: "archived",
					Delivery: &DeliveryConfig{BoardVisible: true},
				},
			},
		},
		namingWarnings: []NamingWarning{{ProgramID: "live", Field: "id", Message: "demo"}},
	}

	rec := httptest.NewRecorder()
	h.HandlePrograms(rec, httptest.NewRequest(http.MethodGet, "/api/v1/programs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body programsListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Programs) != 2 {
		t.Fatalf("default list=%+v", body.Programs)
	}
	for _, p := range body.Programs {
		if p.ID == "old" {
			t.Fatal("archived program leaked into default list")
		}
	}
	if len(body.NamingWarnings) != 1 || body.NamingWarnings[0].ProgramID != "live" {
		t.Fatalf("naming_warnings=%+v", body.NamingWarnings)
	}

	rec2 := httptest.NewRecorder()
	h.HandlePrograms(rec2, httptest.NewRequest(http.MethodGet, "/api/v1/programs?include_archived=true", nil))
	var body2 programsListBody
	if err := json.Unmarshal(rec2.Body.Bytes(), &body2); err != nil {
		t.Fatal(err)
	}
	if len(body2.Programs) != 3 {
		t.Fatalf("include_archived list=%+v", body2.Programs)
	}

	rec3 := httptest.NewRecorder()
	h.HandlePrograms(rec3, httptest.NewRequest(http.MethodGet, "/api/v1/programs?status=active", nil))
	var body3 programsListBody
	if err := json.Unmarshal(rec3.Body.Bytes(), &body3); err != nil {
		t.Fatal(err)
	}
	if len(body3.Programs) != 1 || body3.Programs[0].ID != "live" {
		t.Fatalf("status=active list=%+v", body3.Programs)
	}

	rec4 := httptest.NewRecorder()
	h.HandlePrograms(rec4, httptest.NewRequest(http.MethodGet, "/api/v1/programs?status=completed", nil))
	var body4 programsListBody
	if err := json.Unmarshal(rec4.Body.Bytes(), &body4); err != nil {
		t.Fatal(err)
	}
	if len(body4.Programs) != 1 || body4.Programs[0].ID != "done" {
		t.Fatalf("status=completed list=%+v", body4.Programs)
	}
}

func TestValidateNewProgramID(t *testing.T) {
	if err := ValidateNewProgramID("trade-stack-20260809"); err != nil {
		t.Fatalf("expected valid id: %v", err)
	}
	cases := []string{
		"governance",
		"a",
		"control-room-ui--rocket-build",
		"base--instance",
		strings.Repeat("a", 41),
		strings.Repeat("ab-", 14) + "c", // >40
	}
	for _, id := range cases {
		if err := ValidateNewProgramID(id); err == nil {
			t.Fatalf("expected invalid id %q", id)
		}
	}
	longOK := "trade-stack-20260809-2"
	if err := ValidateNewProgramID(longOK); err != nil {
		t.Fatalf("suffix collision id should pass: %v", err)
	}
}

func TestCanonicalPhaseIDAliases(t *testing.T) {
	bp := &ProgramBlueprint{
		ID: "trade-ib-client-migration",
		Phases: []PhaseBlueprint{
			{ID: "P1", Aliases: []string{"TIBM0"}},
			{ID: "P2", Aliases: []string{"TIBM1"}},
		},
	}
	if got := canonicalPhaseID(bp, "TIBM0"); got != "P1" {
		t.Fatalf("got %s", got)
	}
	if got := canonicalPhaseID(bp, "P2"); got != "P2" {
		t.Fatalf("got %s", got)
	}
	if !phaseExists(bp, "TIBM1") {
		t.Fatal("alias should exist")
	}
}
