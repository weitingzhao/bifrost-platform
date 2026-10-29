package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/opsfacts"
)

func TestOpsFactReaderUsesOnlyBoundedReadAllowlist(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := opsFactReader{handlers: map[opsfacts.Source]http.HandlerFunc{opsfacts.HealthSource: func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/health" || r.Context().Err() != context.Canceled {
			t.Error("read context or path lost")
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"private upstream detail"}`))
	}}}
	got := reader.Read(ctx, opsfacts.HealthSource)
	if got.Status != http.StatusForbidden {
		t.Fatal(got)
	}
	if reader.Read(ctx, opsfacts.Source{Method: "POST", Path: "/health"}).Status != 0 {
		t.Fatal("unlisted source accepted")
	}
	reader.handlers[opsfacts.HealthSource] = func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(strings.Repeat("x", (4<<20)+1))) }
	if reader.Read(ctx, opsfacts.HealthSource).Err == nil {
		t.Fatal("unbounded source body")
	}
	reader.handlers[opsfacts.HealthSource] = func(http.ResponseWriter, *http.Request) {}
	if reader.Read(ctx, opsfacts.HealthSource).Status != http.StatusOK {
		t.Fatal("empty HTTP response must still be 200, then fail JSON validation")
	}
}

// Serial: newTestConfig changes process environment and the global state backend.
func TestOpsSystemRouteUsesViewerGateAndExistingSources(t *testing.T) {
	t.Setenv("AGENTRT_DSN", "")
	cfg := newTestConfig(t)
	srv := newTestServer(t, cfg)
	router := srv.Router().(chi.Routes)
	found := false
	if err := chi.Walk(router, func(method, path string, _ http.Handler, mws ...func(http.Handler) http.Handler) error {
		if path != "/api/v1/ops/facts/system" {
			return nil
		}
		found = true
		if method != http.MethodGet || reachesHandlerAnonymously(method, mws) {
			t.Error("system facts must be a gated GET")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("route missing")
	}
	// Only the in-process health source is executed here. Neither the router
	// sentinel above nor this read accesses a database or network.
	result := srv.opsFactSources().Read(context.Background(), opsfacts.HealthSource)
	var h opsfacts.Health
	if result.Status != 200 || json.Unmarshal(result.Body, &h) != nil || h.Service != "bifrost-platform-api" {
		t.Fatal("health source not wired")
	}
	response := httptest.NewRecorder()
	srv.Router().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/ops/facts/system", nil))
	if response.Code != 401 {
		t.Fatalf("anonymous status: %d", response.Code)
	}
}
