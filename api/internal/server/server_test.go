package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

const fixtureEnvironmentsYAML = `
environments:
  - id: dev
    label: Dev
    nginx_base: http://127.0.0.1:8080
`

const fixtureTopologyYAML = `
deployment_phase: k3s_partial
nodes:
  - id: node-a
    label: Node A
    host: 10.0.0.1
    group: linux
    grid: { row: 1, col: 2 }
edges: []
`

const fixtureClustersYAML = `
clusters:
  - id: test-cluster
    label: Test Cluster
    distribution: k3s
    api_server: https://10.0.0.1:6443
    node_ip: 10.0.0.1
`

const fixtureOpsContextYAML = `
meta:
  version: "v1"
  catalog_version: "v1"
deployment:
  phase: k3s_partial
focus:
  headline: "test focus"
milestones:
  - id: m1
    status: SIGNED
`

// newTestConfig builds a minimal-but-complete config tree in a temp directory
// so server.New can construct every sub-handler without touching the real
// repo config or writing to shared data directories.
func newTestConfig(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(filepath.Join(configDir, "programs"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"environments.yaml": fixtureEnvironmentsYAML,
		"topology.yaml":     fixtureTopologyYAML,
		"clusters.yaml":     fixtureClustersYAML,
		"ops-context.yaml":  fixtureOpsContextYAML,
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(configDir, name), []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	// devagent.NewHandler requires at least one non-underscore program blueprint.
	if err := os.WriteFile(filepath.Join(configDir, "programs", "smoke-test.yaml"), []byte(`
id: smoke-test
title: Smoke Test Program
description: minimal fixture blueprint for server smoke tests
status: active
phases:
  - id: p1
    title: Phase 1
`), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PLATFORM_CONFIG", filepath.Join(configDir, "environments.yaml"))
	t.Setenv("PLATFORM_DATA_DIR", filepath.Join(dir, "data"))
	t.Setenv("PLATFORM_AUTH_CONFIG", filepath.Join(dir, "does-not-exist-platform-auth.yaml"))
	// Keep the test hermetic — without these, several stores/clients fall back
	// to the operator's real $HOME paths (remediation jobs, audit log, kubeconfig)
	// which could leak host state into test assertions or reach a live cluster.
	t.Setenv("PLATFORM_REMEDIATION_JOBS_DIR", filepath.Join(dir, "remediation-jobs"))
	t.Setenv("PLATFORM_AUDIT_LOG", filepath.Join(dir, "audit.json"))
	t.Setenv("PLATFORM_KUBECONFIG", filepath.Join(dir, "does-not-exist-kubeconfig.yaml"))
	t.Setenv("PATROL_DISPATCH", "stub")
	t.Setenv("PATROL_STATE_DIR", filepath.Join(dir, "patrol-state"))
	t.Setenv("PATROL_SKILLS_DIR", filepath.Join(configDir, "patrol-skills"))

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return cfg
}

func TestNewBuildsServerAndHealthRoute(t *testing.T) {
	cfg := newTestConfig(t)
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	router := srv.Router()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Status          string `json:"status"`
		Service         string `json:"service"`
		ContainedPanics int64  `json:"contained_panics"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Status != "ok" || payload.Service != "bifrost-platform-api" {
		t.Fatalf("unexpected health payload: %+v", payload)
	}
	// A contained goroutine panic must be visible here, not only in the log:
	// /health saying ok while a worker died mid-flight is the failure we are
	// paying for with safego.
	if payload.ContainedPanics != safego.Contained() {
		t.Fatalf("contained_panics = %d, want %d", payload.ContainedPanics, safego.Contained())
	}
}

func TestRouterRegistersExpectedPublicRoutes(t *testing.T) {
	cfg := newTestConfig(t)
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	router := srv.Router()

	// GET routes that are safe to hit without auth or live cluster/K8s access —
	// each should be routed (not 404) even though downstream data may be empty.
	getRoutes := []string{
		"/health",
		"/api/v1/environments",
		"/api/v1/context",
		"/api/v1/agent/retrospective/report",
		"/api/v1/agent/retrospective/patterns",
		"/api/v1/agent/retrospective/insights",
		"/api/v1/agent/retrospective/defects",
		"/api/v1/vision/v1/gate",
		"/api/v1/audit",
		"/api/v1/jobs",
		"/api/v1/patrol/skills",
		"/api/v1/patrol/runs",
		"/api/v1/hermes/insights",
		"/api/v1/agent/hermes/readiness",
		"/api/v1/agent/hermes/first-task",
	}
	for _, path := range getRoutes {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code == http.StatusNotFound {
			t.Fatalf("route %s not registered (404)", path)
		}
	}
}

func TestRouterUnknownRouteIs404(t *testing.T) {
	cfg := newTestConfig(t)
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	router := srv.Router()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/does-not-exist", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for unknown route", rec.Code)
	}
}

func TestRouterRequiresAuthForOperatorRoutes(t *testing.T) {
	cfg := newTestConfig(t)
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	router := srv.Router()

	// Without a platform-auth.yaml, the AuthService has no principals — an
	// operator-gated route must reject unauthenticated requests, not panic.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/nightly-run", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("expected operator-gated route to reject unauthenticated request, got 200")
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/patrol/trigger/fleet-drift-scan", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("expected patrol trigger to reject unauthenticated request, got 200")
	}
}

func TestHandleEnvironmentsListsConfiguredEnvironments(t *testing.T) {
	cfg := newTestConfig(t)
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	router := srv.Router()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Environments []map[string]string `json:"environments"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload.Environments) != 1 || payload.Environments[0]["id"] != "dev" {
		t.Fatalf("environments payload = %+v", payload.Environments)
	}
}

// The split exists so the always-on loops run exactly once. Before it, every
// replica started its own patrol autopilot, IB auto-repair loop and hourly
// data-clone scheduler; prod runs two replicas, so each of those ran twice
// against per-pod state that could not see the other run.
func TestRoleDecidesWhoRunsTheBackgroundLoops(t *testing.T) {
	for _, tc := range []struct {
		role  string
		loops bool
	}{
		{"api", false},
		{"workers", true},
		{"", true}, // unset stays all-in-one: local make start is unchanged
	} {
		t.Run("role="+tc.role, func(t *testing.T) {
			t.Setenv(config.RoleEnv, tc.role)
			srv, err := New(newTestConfig(t))
			if err != nil {
				t.Fatalf("server.New: %v", err)
			}
			t.Cleanup(func() { srv.plane.StopBackground() })

			if got := srv.plane.Patrol().Running(); got != tc.loops {
				t.Fatalf("patrol autopilot running = %v, want %v", got, tc.loops)
			}

			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			rec := httptest.NewRecorder()
			srv.Router().ServeHTTP(rec, req)
			var payload struct {
				Role            string `json:"role"`
				BackgroundLoops bool   `json:"background_loops"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if payload.BackgroundLoops != tc.loops {
				t.Fatalf("health background_loops = %v, want %v", payload.BackgroundLoops, tc.loops)
			}
			// /health names the role so an operator can tell the two pods apart.
			wantRole := tc.role
			if wantRole == "" {
				wantRole = string(config.RoleAll)
			}
			if payload.Role != wantRole {
				t.Fatalf("health role = %q, want %q", payload.Role, wantRole)
			}
		})
	}
}
