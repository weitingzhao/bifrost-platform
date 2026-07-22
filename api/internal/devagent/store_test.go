package devagent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileStoreSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

	store := NewFileStore(configDir)
	phases := []Phase{
		{ID: "P0", Title: "Phase 0", Status: PhaseDone, CompletedAt: "2026-07-04T00:00:00Z"},
		{ID: "P1", Title: "Phase 1", Status: PhaseRunning, StartedAt: "2026-07-04T01:00:00Z"},
	}
	job := &Job{ID: "job-1", PhaseID: "P1", Status: JobRunning, Output: "running..."}
	history := []Job{{ID: "job-0", PhaseID: "P0", Status: JobDone, CompletedAt: "2026-07-04T00:00:00Z"}}

	if err := store.SaveProgram("test-program", phases, job, history); err != nil {
		t.Fatalf("SaveProgram: %v", err)
	}
	if err := store.SaveActiveProgramID("test-program"); err != nil {
		t.Fatalf("SaveActiveProgramID: %v", err)
	}

	activeID, err := store.LoadActiveProgramID()
	if err != nil || activeID != "test-program" {
		t.Fatalf("LoadActiveProgramID = %q err=%v", activeID, err)
	}

	rec, err := store.LoadProgram("test-program")
	if err != nil {
		t.Fatalf("LoadProgram: %v", err)
	}
	if rec == nil {
		t.Fatal("expected program record")
	}
	if len(rec.Phases) != 2 || rec.Phases[1].Status != PhaseRunning {
		t.Fatalf("phases not persisted: %+v", rec.Phases)
	}
	if rec.ActiveJob == nil || rec.ActiveJob.ID != "job-1" {
		t.Fatalf("active job not persisted: %+v", rec.ActiveJob)
	}
	if len(rec.History) != 1 {
		t.Fatalf("history not persisted: %+v", rec.History)
	}

	data, err := os.ReadFile(store.programPath("test-program"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	if !strings.Contains(string(data), `"program_id"`) || !strings.Contains(string(data), `"phases"`) {
		t.Fatalf("state file not human-readable JSON: %s", string(data))
	}

	info, err := store.ListInfo("test-program")
	if err != nil {
		t.Fatalf("ListInfo: %v", err)
	}
	if len(info.Files) != 1 || info.StateDir == "" {
		t.Fatalf("unexpected ListInfo: %+v", info)
	}
}

func TestHandlerPersistenceSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	programsDir := filepath.Join(configDir, "programs")
	if err := os.MkdirAll(programsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := `id: persist-test
title: Persist Test
description: test
status: active
workspace: /tmp
skill_path: .cursor/skills/test/SKILL.md
phases:
  - id: P0
    title: Phase 0
    status: pending
    prompt_template: "run {{phase_id}}"
metadata:
  created_at: "2026-07-04"
  owner: test
`
	if err := os.WriteFile(filepath.Join(programsDir, "persist-test.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	h1, err := NewHandler(configDir)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	h1.mu.Lock()
	rt := h1.runtimes["persist-test"]
	rt.phases[0].Status = PhaseDone
	rt.phases[0].CompletedAt = "2026-07-04T12:00:00Z"
	rt.history = []Job{{ID: "j1", PhaseID: "P0", Status: JobDone}}
	if persistErr := h1.persistRuntimeLocked("persist-test"); persistErr != nil {
		t.Fatalf("persist: %v", persistErr)
	}
	h1.mu.Unlock()

	h2, err := NewHandler(configDir)
	if err != nil {
		t.Fatalf("NewHandler restart: %v", err)
	}
	rt2 := h2.runtimes["persist-test"]
	if rt2.phases[0].Status != PhaseDone {
		t.Fatalf("phase status not restored: %+v", rt2.phases[0])
	}
	if len(rt2.history) != 1 {
		t.Fatalf("history not restored: %+v", rt2.history)
	}
}

func TestMergePhasesFromState(t *testing.T) {
	bp := &ProgramBlueprint{
		Phases: []PhaseBlueprint{
			{ID: "A", Title: "A", Status: "pending"},
			{ID: "B", Title: "B", Status: "pending"},
		},
	}
	merged := mergePhasesFromState(bp, []Phase{
		{ID: "A", Status: PhaseDone, CompletedAt: "t1"},
	})
	if merged[0].Status != PhaseDone || merged[1].Status != PhasePending {
		t.Fatalf("merge failed: %+v", merged)
	}
}