package devagent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const signoffTestProgramYAML = `id: signoff-test
title: Signoff Test
description: test
status: active
phases:
  - id: P1
    title: Phase 1
    status: pending
    sign_off:
      required: true
  - id: V1
    title: V1
    status: pending
    sign_off:
      required: true
metadata:
  created_at: "2026-07-07"
  owner: test
`

func newSignoffTestHandler(t *testing.T) (*Handler, string) {
	t.Helper()
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	programsDir := filepath.Join(configDir, "programs")
	if err := os.MkdirAll(programsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(programsDir, "signoff-test.yaml"), []byte(signoffTestProgramYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(configDir)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	return h, configDir
}

func TestRecordPhaseSignoffWritesPhaseSignOffs(t *testing.T) {
	h, _ := newSignoffTestHandler(t)

	const signedAt = "2026-07-07T12:00:00Z"
	if err := h.RecordPhaseSignoff("signoff-test", "P1", "owner", signedAt, "verified"); err != nil {
		t.Fatalf("RecordPhaseSignoff: %v", err)
	}

	rt := h.runtimes["signoff-test"]
	rec := h.phaseSignoffRecordLocked(rt, "P1")
	if rec == nil || rec.SignedOffAt != signedAt || rec.SignedOffBy != "owner" || rec.Notes != "verified" {
		t.Fatalf("phase sign-off not persisted: %+v", rec)
	}
	if rt.phases[0].Status != PhaseDone || rt.phases[0].CompletedAt != signedAt {
		t.Fatalf("phase status not updated: %+v", rt.phases[0])
	}
}

func TestRecordPhaseSignoffDuplicateReturnsAlreadySignedOff(t *testing.T) {
	h, _ := newSignoffTestHandler(t)

	if err := h.RecordPhaseSignoff("signoff-test", "P1", "owner", "2026-07-07T12:00:00Z", "first"); err != nil {
		t.Fatalf("first signoff: %v", err)
	}
	err := h.RecordPhaseSignoff("signoff-test", "P1", "owner", "2026-07-07T13:00:00Z", "second")
	if err == nil {
		t.Fatal("expected duplicate signoff error")
	}
	if !strings.Contains(err.Error(), "already signed off") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyPhaseSignoffLockedRFC3339Validation(t *testing.T) {
	h, _ := newSignoffTestHandler(t)

	rt := h.runtimes["signoff-test"]
	h.mu.Lock()
	defer h.mu.Unlock()
	err := h.applyPhaseSignoffLocked(rt, "P1", "owner", "not-a-timestamp", "notes")
	if err == nil {
		t.Fatal("expected RFC3339 validation error")
	}
	if !strings.Contains(err.Error(), "RFC3339") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSyncVisionSignoffsFromGateFilesSkipsAlreadySigned(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	programsDir := filepath.Join(configDir, "programs")
	if err := os.MkdirAll(programsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	visionYAML := `id: vision
title: Vision
description: test
status: active
phases:
  - id: V1
    title: V1
    status: pending
    sign_off:
      required: true
metadata:
  created_at: "2026-07-07"
  owner: test
`
	if err := os.WriteFile(filepath.Join(programsDir, "vision.yaml"), []byte(visionYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	h, err := NewHandler(configDir)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	const existingAt = "2026-07-06T10:00:00Z"
	if err := h.RecordPhaseSignoff("vision", "V1", "programs-owner", existingAt, "already signed"); err != nil {
		t.Fatalf("pre-sign V1: %v", err)
	}

	gateAt := time.Date(2026, 7, 7, 15, 0, 0, 0, time.UTC)
	gateRec := visionGateSignoffRecord{
		At:       gateAt,
		SignedBy: "gate-owner",
		Notes:    "from gate file",
		Result:   "SIGNED",
	}
	gateData, err := json.Marshal(gateRec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "vision_v1_gate_signoff.json"), gateData, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := h.syncVisionSignoffsFromGateFiles(); err != nil {
		t.Fatalf("syncVisionSignoffsFromGateFiles: %v", err)
	}

	rt := h.runtimes["vision"]
	rec := h.phaseSignoffRecordLocked(rt, "V1")
	if rec == nil {
		t.Fatal("expected existing sign-off record")
	}
	if rec.SignedOffAt != existingAt || rec.SignedOffBy != "programs-owner" || rec.Notes != "already signed" {
		t.Fatalf("sign-off overwritten by gate sync: %+v", rec)
	}
}
