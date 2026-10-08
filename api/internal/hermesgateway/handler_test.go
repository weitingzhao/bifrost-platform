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
