package driftproposal

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

func requestWithID(method, path, body, id string) *http.Request {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, bytes.NewBufferString(body))
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func newTestDriftHandler(t *testing.T, runnerURL string) *Handler {
	t.Helper()
	t.Setenv("PLATFORM_DRIFT_PROPOSALS_DIR", t.TempDir())
	t.Setenv("REMEDIATION_RUNNER_URL", runnerURL)
	t.Setenv("REMEDIATION_RUNNER_STANDBY_URL", "")
	return NewHandler(actuation.NewAuditLog(""))
}

func TestHandleCreateValidatesRequiredFields(t *testing.T) {
	h := newTestDriftHandler(t, "http://127.0.0.1:0")

	cases := []struct {
		name string
		body string
	}{
		{"missing_layers", `{"summary":"drift found"}`},
		{"missing_summary", `{"layers_failed":["catalog"]}`},
		{"invalid_json", `not-json`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/drift-proposals", bytes.NewBufferString(tc.body))
			rec := httptest.NewRecorder()
			h.HandleCreate(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHandleCreateThenGetAndList(t *testing.T) {
	h := newTestDriftHandler(t, "http://127.0.0.1:0")

	createBody := `{"host":"mac-mini-1","layers_failed":["catalog","docs"],"findings_count":2,"summary":"catalog drift"}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/drift-proposals", bytes.NewBufferString(createBody))
	createRec := httptest.NewRecorder()
	h.HandleCreate(createRec, createReq)

	if createRec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body=%s", createRec.Code, createRec.Body.String())
	}
	var created Proposal
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if created.Status != StatusPendingApproval || created.Summary != "catalog drift" {
		t.Fatalf("created = %+v", created)
	}

	getReq := requestWithID(http.MethodGet, "/api/v1/drift-proposals/"+created.ID, "", created.ID)
	getRec := httptest.NewRecorder()
	h.HandleGet(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("HandleGet status = %d, body=%s", getRec.Code, getRec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/drift-proposals", nil)
	listRec := httptest.NewRecorder()
	h.HandleList(listRec, listReq)
	var listResp struct {
		Proposals []Proposal `json:"proposals"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if len(listResp.Proposals) != 1 || listResp.Proposals[0].ID != created.ID {
		t.Fatalf("listResp = %+v", listResp)
	}
}

func TestHandleGetNotFound(t *testing.T) {
	h := newTestDriftHandler(t, "http://127.0.0.1:0")

	req := requestWithID(http.MethodGet, "/api/v1/drift-proposals/nowhere", "", "nowhere")
	rec := httptest.NewRecorder()
	h.HandleGet(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHandleApproveStartsRemediationAndUpdatesStatus(t *testing.T) {
	runner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/run" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"id":"job-drift-1","status":"running"}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(runner.Close)
	h := newTestDriftHandler(t, runner.URL)

	h.store.mustPut(t, Proposal{ID: "drift-1", Status: StatusPendingApproval, Summary: "s", LayersFailed: []string{"catalog"}})

	req := requestWithID(http.MethodPost, "/api/v1/drift-proposals/drift-1/approve", "", "drift-1")
	rec := httptest.NewRecorder()
	h.HandleApprove(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202, body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Proposal       Proposal `json:"proposal"`
		RemediationJob struct {
			ID string `json:"id"`
		} `json:"remediation_job"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Proposal.Status != StatusRunning || payload.Proposal.RemediationJobID != "job-drift-1" {
		t.Fatalf("proposal = %+v", payload.Proposal)
	}
	if payload.RemediationJob.ID != "job-drift-1" {
		t.Fatalf("remediation_job = %+v", payload.RemediationJob)
	}

	got, ok := h.store.Get("drift-1")
	if !ok || got.Status != StatusRunning {
		t.Fatalf("stored proposal = %+v ok=%v, want running", got, ok)
	}
}

func TestHandleApproveRejectsWhenNotPending(t *testing.T) {
	h := newTestDriftHandler(t, "http://127.0.0.1:0")
	h.store.mustPut(t, Proposal{ID: "drift-1", Status: StatusApproved})

	req := requestWithID(http.MethodPost, "/api/v1/drift-proposals/drift-1/approve", "", "drift-1")
	rec := httptest.NewRecorder()
	h.HandleApprove(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleApproveRunnerUnavailableLeavesProposalPending(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	dead.Close()
	h := newTestDriftHandler(t, dead.URL)
	h.store.mustPut(t, Proposal{ID: "drift-1", Status: StatusPendingApproval})

	req := requestWithID(http.MethodPost, "/api/v1/drift-proposals/drift-1/approve", "", "drift-1")
	rec := httptest.NewRecorder()
	h.HandleApprove(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502, body=%s", rec.Code, rec.Body.String())
	}
	got, _ := h.store.Get("drift-1")
	if got.Status != StatusPendingApproval {
		t.Fatalf("proposal status = %q, want unchanged pending_approval", got.Status)
	}
}

func TestHandleRejectSetsRejectedStatus(t *testing.T) {
	h := newTestDriftHandler(t, "http://127.0.0.1:0")
	h.store.mustPut(t, Proposal{ID: "drift-1", Status: StatusPendingApproval})

	req := requestWithID(http.MethodPost, "/api/v1/drift-proposals/drift-1/reject", `{"note":"false positive"}`, "drift-1")
	rec := httptest.NewRecorder()
	h.HandleReject(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var p Proposal
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Status != StatusRejected || p.RejectNote != "false positive" {
		t.Fatalf("p = %+v", p)
	}
}

func TestHandleRejectRejectsWhenNotPending(t *testing.T) {
	h := newTestDriftHandler(t, "http://127.0.0.1:0")
	h.store.mustPut(t, Proposal{ID: "drift-1", Status: StatusRejected})

	req := requestWithID(http.MethodPost, "/api/v1/drift-proposals/drift-1/reject", `{}`, "drift-1")
	rec := httptest.NewRecorder()
	h.HandleReject(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestBuildAutofixPromptIncludesProposalDetails(t *testing.T) {
	p := Proposal{ID: "drift-1", LayersFailed: []string{"catalog", "docs"}, FindingsCount: 4, ReportSource: "nightly.sh", Summary: "catalog drift found"}
	prompt := buildAutofixPrompt(p)
	for _, want := range []string{"drift-1", "catalog, docs", "4", "nightly.sh", "catalog drift found"} {
		if !bytes.Contains([]byte(prompt), []byte(want)) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}
