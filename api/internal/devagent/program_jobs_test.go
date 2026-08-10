package devagent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
)

const jobsTestProgramA = `id: jobs-active
title: Jobs Active
description: test
status: active
phases:
  - id: P1
    title: Phase 1
    status: pending
metadata:
  created_at: "2026-08-09"
  owner: test
`

const jobsTestProgramB = `id: jobs-idle
title: Jobs Idle
description: test
status: active
phases:
  - id: P1
    title: Phase 1
    status: pending
metadata:
  created_at: "2026-08-09"
  owner: test
`

func newJobsTestHandler(t *testing.T) *Handler {
	t.Helper()
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	programsDir := filepath.Join(configDir, "programs")
	if err := os.MkdirAll(programsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(programsDir, "jobs-active.yaml"), []byte(jobsTestProgramA), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(programsDir, "jobs-idle.yaml"), []byte(jobsTestProgramB), 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(configDir)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	return h
}

func getProgramJobs(t *testing.T, h *Handler, programID string) (*httptest.ResponseRecorder, ProgramJobsResponse) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/programs/"+programID+"/jobs", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("programId", programID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.HandleProgramJobs(rec, req)
	var body ProgramJobsResponse
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil && rec.Code == http.StatusOK {
			t.Fatalf("decode: %v body=%s", err, rec.Body.String())
		}
	}
	return rec, body
}

func TestHandleProgramJobsReturnsPersistedActiveJobAndHistory(t *testing.T) {
	h := newJobsTestHandler(t)
	const startedAt = "2026-08-01T10:00:00Z"
	h.mu.Lock()
	idle := h.runtimes["jobs-idle"]
	idle.history = []Job{{
		ID: "hist-1", PhaseID: "P1", Status: JobDone,
		Output: "done", StartedAt: startedAt, CompletedAt: "2026-08-01T10:05:00Z",
	}}
	idle.activeJob = &Job{ID: "stale-live", PhaseID: "P1", Status: JobAwaitingReview}
	h.mu.Unlock()

	rec, body := getProgramJobs(t, h, "jobs-idle")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if body.ProgramID != "jobs-idle" {
		t.Fatalf("program_id=%q", body.ProgramID)
	}
	if body.ActiveJob == nil || body.ActiveJob.ID != "stale-live" {
		t.Fatalf("active_job = %+v", body.ActiveJob)
	}
	if len(body.History) != 1 || body.History[0].ID != "hist-1" || body.History[0].StartedAt != startedAt {
		t.Fatalf("history = %+v", body.History)
	}
}

func TestHandleProgramJobsActiveIncludesActiveJob(t *testing.T) {
	h := newJobsTestHandler(t)
	h.mu.Lock()
	rt := h.runtimes["jobs-active"]
	rt.activeJob = &Job{ID: "live-1", PhaseID: "P1", Status: JobRunning, StartedAt: "2026-08-09T12:00:00Z"}
	rt.history = []Job{{ID: "old-1", PhaseID: "P1", Status: JobDone, CompletedAt: "2026-08-08T12:00:00Z"}}
	h.mu.Unlock()

	rec, body := getProgramJobs(t, h, "jobs-active")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if body.ActiveJob == nil || body.ActiveJob.ID != "live-1" {
		t.Fatalf("active_job = %+v", body.ActiveJob)
	}
	if len(body.History) != 1 || body.History[0].ID != "old-1" {
		t.Fatalf("history = %+v", body.History)
	}
}

func TestHandleProgramJobsUnknownIDNotFound(t *testing.T) {
	h := newJobsTestHandler(t)
	rec, _ := getProgramJobs(t, h, "does-not-exist")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleProgramJobsEmptyHistoryIsArray(t *testing.T) {
	h := newJobsTestHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/programs/jobs-idle/jobs", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("programId", "jobs-idle")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	h.HandleProgramJobs(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["history"]) != "[]" {
		t.Fatalf("history JSON = %s, want []", raw["history"])
	}
	if string(raw["active_job"]) != "null" {
		t.Fatalf("active_job JSON = %s, want null", raw["active_job"])
	}
}
