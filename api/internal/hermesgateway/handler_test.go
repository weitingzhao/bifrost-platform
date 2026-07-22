package hermesgateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleHealthNotConfigured(t *testing.T) {
	t.Setenv("HERMES_GATEWAY_URL", "")
	h := NewHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/hermes-gateway/health", nil)
	rec := httptest.NewRecorder()
	h.HandleHealth(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleHealthProxiesGateway(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(gw.Close)
	t.Setenv("HERMES_GATEWAY_URL", gw.URL)
	h := NewHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/hermes-gateway/health", nil)
	rec := httptest.NewRecorder()
	h.HandleHealth(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestHandleHealthPropagatesUpstreamErrorStatus(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	t.Cleanup(gw.Close)
	t.Setenv("HERMES_GATEWAY_URL", gw.URL)
	h := NewHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/hermes-gateway/health", nil)
	rec := httptest.NewRecorder()
	h.HandleHealth(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (proxied)", rec.Code)
	}
}

func TestHandleSkillsNotConfiguredReturnsEmptyList(t *testing.T) {
	t.Setenv("HERMES_GATEWAY_URL", "")
	h := NewHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/hermes-gateway/skills", nil)
	rec := httptest.NewRecorder()
	h.HandleSkills(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["gateway_status"] != "not_configured" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestHandleSchedulesNotConfiguredReturnsEmptyList(t *testing.T) {
	t.Setenv("HERMES_GATEWAY_URL", "")
	h := NewHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/hermes-gateway/schedules", nil)
	rec := httptest.NewRecorder()
	h.HandleSchedules(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Schedules []any `json:"schedules"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Schedules == nil || len(payload.Schedules) != 0 {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestHandleExecutionsForwardsQueryString(t *testing.T) {
	var gotQuery string
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"executions":[],"total":0}`))
	}))
	t.Cleanup(gw.Close)
	t.Setenv("HERMES_GATEWAY_URL", gw.URL)
	h := NewHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/hermes-gateway/executions?limit=10", nil)
	rec := httptest.NewRecorder()
	h.HandleExecutions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if gotQuery != "limit=10" {
		t.Fatalf("gateway received query %q, want limit=10", gotQuery)
	}
}

func TestHandleSkillActuationLevelNotConfigured(t *testing.T) {
	t.Setenv("HERMES_GATEWAY_URL", "")
	h := NewHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/hermes-gateway/skills/level", nil)
	rec := httptest.NewRecorder()
	h.HandleSkillActuationLevel(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestHandleSkillActuationLevelNotImplementedWhenConfigured(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(gw.Close)
	t.Setenv("HERMES_GATEWAY_URL", gw.URL)
	h := NewHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/hermes-gateway/skills/level", nil)
	rec := httptest.NewRecorder()
	h.HandleSkillActuationLevel(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", rec.Code)
	}
}
