package remediation

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

func requestWithParam(method, path, body, key, value string) *http.Request {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, bytes.NewBufferString(body))
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// newTestHandler wires a Handler against a fake runner server and an
// isolated job-store temp dir so each test is hermetic.
func newTestHandler(t *testing.T, runnerURL string) *Handler {
	t.Helper()
	t.Setenv("REMEDIATION_RUNNER_URL", runnerURL)
	t.Setenv("REMEDIATION_RUNNER_STANDBY_URL", "")
	t.Setenv("PLATFORM_REMEDIATION_JOBS_DIR", t.TempDir())
	return NewHandler(actuation.NewAuditLog(""))
}

func TestHandleStartPersistsJobToStore(t *testing.T) {
	srv := newFakeRunnerServer(t)
	h := newTestHandler(t, srv.URL)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/remediation/start", bytes.NewBufferString(`{"scope":"test-scope"}`))
	rec := httptest.NewRecorder()
	h.HandleStart(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var job Job
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if job.ID != "job-1" {
		t.Fatalf("job.ID = %q, want job-1", job.ID)
	}
	stored, ok := h.Store().Get("job-1")
	if !ok || stored.Scope != "test-scope" {
		t.Fatalf("expected job persisted to store with scope, got %+v ok=%v", stored, ok)
	}
}

func TestHandleStartInvalidJSONReturnsBadRequest(t *testing.T) {
	srv := newFakeRunnerServer(t)
	h := newTestHandler(t, srv.URL)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/remediation/start", bytes.NewBufferString("not json"))
	rec := httptest.NewRecorder()
	h.HandleStart(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleStartRunnerUnavailableReturnsBadGateway(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	dead.Close()
	h := newTestHandler(t, dead.URL)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/remediation/start", bytes.NewBufferString(`{"scope":"x"}`))
	rec := httptest.NewRecorder()
	h.HandleStart(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleGetReturnsRunnerJobAndUpdatesStore(t *testing.T) {
	srv := newFakeRunnerServer(t)
	h := newTestHandler(t, srv.URL)

	req := requestWithParam(http.MethodGet, "/api/v1/remediation/job-1", "", "id", "job-1")
	rec := httptest.NewRecorder()
	h.HandleGet(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var job Job
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if job.Status != JobDone {
		t.Fatalf("job.Status = %q, want done", job.Status)
	}
	if stored, ok := h.Store().Get("job-1"); !ok || stored.Status != JobDone {
		t.Fatalf("expected store to reflect runner job, got %+v ok=%v", stored, ok)
	}
}

func TestHandleGetFallsBackToStoreWhenNotFoundOnRunner(t *testing.T) {
	srv := newFakeRunnerServer(t)
	h := newTestHandler(t, srv.URL)
	h.Store().Put(Job{ID: "job-missing", Status: JobFailed, Summary: "archived only"})

	req := requestWithParam(http.MethodGet, "/api/v1/remediation/job-missing", "", "id", "job-missing")
	rec := httptest.NewRecorder()
	h.HandleGet(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var job Job
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if job.Summary != "archived only" {
		t.Fatalf("expected archived store copy, got %+v", job)
	}
}

func TestHandleGetReturns404WhenUnknownEverywhere(t *testing.T) {
	srv := newFakeRunnerServer(t)
	h := newTestHandler(t, srv.URL)

	req := requestWithParam(http.MethodGet, "/api/v1/remediation/nowhere", "", "id", "nowhere")
	rec := httptest.NewRecorder()
	h.HandleGet(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleListMergesRunnerAndStore(t *testing.T) {
	srv := newFakeRunnerServer(t)
	h := newTestHandler(t, srv.URL)
	h.Store().Put(Job{ID: "archived-only", Status: JobDone})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/remediation/jobs", nil)
	rec := httptest.NewRecorder()
	h.HandleList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Jobs []Job `json:"jobs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ids := map[string]bool{}
	for _, j := range payload.Jobs {
		ids[j.ID] = true
	}
	if !ids["job-1"] || !ids["archived-only"] {
		t.Fatalf("expected merged jobs to include both runner and archive-only jobs, got %+v", payload.Jobs)
	}
}

func TestHandleCancelSuccess(t *testing.T) {
	srv := newFakeRunnerServer(t)
	h := newTestHandler(t, srv.URL)

	req := requestWithParam(http.MethodPost, "/api/v1/remediation/job-1/cancel", "", "id", "job-1")
	rec := httptest.NewRecorder()
	h.HandleCancel(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var job Job
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if job.Status != JobCancelled {
		t.Fatalf("job.Status = %q, want cancelled", job.Status)
	}
}

func TestHandleCancelDismissesOrphanWhenRunnerUnreachable(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	dead.Close()
	h := newTestHandler(t, dead.URL)
	h.Store().Put(Job{ID: "orphan-1", Status: JobRunning})

	req := requestWithParam(http.MethodPost, "/api/v1/remediation/orphan-1/cancel", "", "id", "orphan-1")
	rec := httptest.NewRecorder()
	h.HandleCancel(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var job Job
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if job.Status != JobCancelled || job.Error != "orphaned" {
		t.Fatalf("job = %+v, want cancelled/orphaned", job)
	}
}

func TestHandleRespondRequiresOptionID(t *testing.T) {
	srv := newFakeRunnerServer(t)
	h := newTestHandler(t, srv.URL)

	req := requestWithParam(http.MethodPost, "/api/v1/remediation/job-1/respond", `{}`, "id", "job-1")
	rec := httptest.NewRecorder()
	h.HandleRespond(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleRespondSuccess(t *testing.T) {
	srv := newFakeRunnerServer(t)
	h := newTestHandler(t, srv.URL)

	req := requestWithParam(http.MethodPost, "/api/v1/remediation/job-1/respond", `{"option_id":"opt-1"}`, "id", "job-1")
	rec := httptest.NewRecorder()
	h.HandleRespond(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["ok"] != true {
		t.Fatalf("payload = %+v, want ok=true", payload)
	}
}

func TestHandleHealthSuccess(t *testing.T) {
	srv := newFakeRunnerServer(t)
	h := newTestHandler(t, srv.URL)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/remediation/health", nil)
	rec := httptest.NewRecorder()
	h.HandleHealth(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleHealthUnavailable(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	dead.Close()
	h := newTestHandler(t, dead.URL)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/remediation/health", nil)
	rec := httptest.NewRecorder()
	h.HandleHealth(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

// terminalRecorder captures OnRemediationTerminal notifications for
// TestHandleStreamNotifiesTerminalObserverAndUpdatesStore.
type terminalRecorder struct {
	jobs []Job
}

func (r *terminalRecorder) OnRemediationTerminal(job Job) {
	r.jobs = append(r.jobs, job)
}

func TestHandleStreamNotifiesTerminalObserverAndUpdatesStore(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/run/job-1/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"job\",\"job\":{\"id\":\"job-1\",\"status\":\"done\",\"summary\":\"ok\"}}\n\n"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	h := newTestHandler(t, srv.URL)
	obs := &terminalRecorder{}
	h.BindTerminalObserver(obs)

	req := requestWithParam(http.MethodGet, "/api/v1/remediation/job-1/stream", "", "id", "job-1")
	rec := httptest.NewRecorder()
	h.HandleStream(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() == 0 {
		t.Fatal("expected SSE body to be forwarded to the client")
	}
	stored, ok := h.Store().Get("job-1")
	if !ok || stored.Status != JobDone {
		t.Fatalf("expected stream to persist job to store, got %+v ok=%v", stored, ok)
	}
	if len(obs.jobs) != 1 || obs.jobs[0].ID != "job-1" {
		t.Fatalf("expected terminal observer to be notified once, got %+v", obs.jobs)
	}
}
