package buildgate

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/opscontext"
)

func requestWithPhase(method, path, body, phase string) *http.Request {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, bytes.NewBufferString(body))
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("phase", phase)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestHandleGetGateReturnsPhaseResponse(t *testing.T) {
	cfg := newTestConfig(t, []opscontext.TrackTask{{ID: "p1-1", Label: "Task 1", Status: "done"}})
	h := NewHandler(cfg, actuation.NewAuditLog(""))

	req := requestWithPhase(http.MethodGet, "/api/v1/build-gate/p1", "", "p1")
	rec := httptest.NewRecorder()
	h.HandleGetGate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp PhaseGateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Phase != "P1" || !resp.Ready {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestHandleRunGateThenSignoffFlow(t *testing.T) {
	cfg := newTestConfig(t, []opscontext.TrackTask{{ID: "p1-1", Label: "Task 1", Status: "done"}})
	h := NewHandler(cfg, actuation.NewAuditLog(""))

	runReq := requestWithPhase(http.MethodPost, "/api/v1/build-gate/p1/run", "", "p1")
	runRec := httptest.NewRecorder()
	h.HandleRunGate(runRec, runReq)
	if runRec.Code != http.StatusOK {
		t.Fatalf("HandleRunGate status = %d, body=%s", runRec.Code, runRec.Body.String())
	}
	var runResp RunGateResponse
	if err := json.Unmarshal(runRec.Body.Bytes(), &runResp); err != nil {
		t.Fatalf("unmarshal run: %v", err)
	}
	if !runResp.OK {
		t.Fatalf("RunGate response OK=false: %+v", runResp)
	}

	signReq := requestWithPhase(http.MethodPost, "/api/v1/build-gate/p1/signoff", `{"notes":"go"}`, "p1")
	signRec := httptest.NewRecorder()
	h.HandleSignoff(signRec, signReq)
	if signRec.Code != http.StatusOK {
		t.Fatalf("HandleSignoff status = %d, body=%s", signRec.Code, signRec.Body.String())
	}
	var signResp RunGateResponse
	if err := json.Unmarshal(signRec.Body.Bytes(), &signResp); err != nil {
		t.Fatalf("unmarshal signoff: %v", err)
	}
	if !signResp.OK {
		t.Fatalf("Signoff response OK=false: %+v", signResp)
	}
	if signResp.Gate.SignedAt == nil {
		t.Fatalf("Signoff response gate missing SignedAt: %+v", signResp.Gate)
	}
}

func TestHandleSignoffRejectsWhenGateNotReady(t *testing.T) {
	cfg := newTestConfig(t, []opscontext.TrackTask{{ID: "p1-1", Label: "Task 1", Status: "blocked"}})
	h := NewHandler(cfg, actuation.NewAuditLog(""))

	req := requestWithPhase(http.MethodPost, "/api/v1/build-gate/p1/signoff", `{}`, "p1")
	rec := httptest.NewRecorder()
	h.HandleSignoff(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleListPhases(t *testing.T) {
	cfg := newTestConfig(t, []opscontext.TrackTask{{ID: "p1-1", Label: "Task 1", Status: "done"}})
	h := NewHandler(cfg, actuation.NewAuditLog(""))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/build-gate", nil)
	rec := httptest.NewRecorder()
	h.HandleListPhases(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp []PhaseGateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp) != 1 || resp[0].Phase != "P1" {
		t.Fatalf("resp = %+v, want single P1 entry", resp)
	}
}
