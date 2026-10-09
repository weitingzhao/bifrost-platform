package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
		"/api/v1/audit",
		"/api/v1/jobs",
		"/api/v1/patrol/skills",
		"/api/v1/patrol/runs",
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

func TestRetiredRoutesAre404(t *testing.T) {
	cfg := newTestConfig(t)
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	router := srv.Router()
	retired := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/operate/queue"},
		{http.MethodPost, "/api/v1/operate/queue"},
		{http.MethodGet, "/api/v1/operate/briefs"},
		{http.MethodGet, "/api/v1/operate/drain/status"},
		{http.MethodPost, "/api/v1/operate/sweep"},
		{http.MethodGet, "/api/v1/vision/v1/gate"},
		{http.MethodPost, "/api/v1/vision/v1/signoff"},
		{http.MethodGet, "/api/v1/vision/s3/gate"},
		{http.MethodGet, "/api/v1/vision/v5/gate"},
		{http.MethodGet, "/api/v1/build-phase"},
		{http.MethodPost, "/api/v1/build-phase/p1/gate"},
		{http.MethodGet, "/api/v1/migrate-streams/catalog"},
		{http.MethodPost, "/api/v1/migrate-streams/s/waves/w/deliver"},
		{http.MethodGet, "/api/v1/promote/release-gate"},
		{http.MethodPost, "/api/v1/promote/release-gate"},
		{http.MethodGet, "/api/v1/promote/release-state"},
		{http.MethodGet, "/api/v1/promote/gate-history"},
		{http.MethodGet, "/api/v1/promote/tier-b"},
		{http.MethodPost, "/api/v1/promote/tier-b/signoff"},
		{http.MethodGet, "/api/v1/hermes/insights"},
		{http.MethodPost, "/api/v1/hermes/run-first-task"},
		{http.MethodGet, "/api/v1/agent/hermes/first-task"},
		{http.MethodGet, "/api/v1/agent/drift-proposals"},
		{http.MethodPost, "/api/v1/agent/drift-proposals"},
		{http.MethodGet, "/api/v1/agent/retrospective/patterns"},
		{http.MethodGet, "/api/v1/agent/retrospective/insights"},
		{http.MethodGet, "/api/v1/agent/retrospective/report"},
		{http.MethodGet, "/api/v1/agent/retrospective/defects"},
		{http.MethodGet, "/api/v1/cluster/join-profiles"},
		{http.MethodPost, "/api/v1/cluster/nodes/join"},
		{http.MethodPost, "/api/v1/checklist/husbandry-sync"},
		{http.MethodGet, "/api/v1/checklist/kpis"},
		{http.MethodGet, "/api/v1/agent-tasks"},
		{http.MethodGet, "/api/v1/agent/governance/performance"},
		{http.MethodGet, "/api/v1/agent/governance/capability-map"},
		{http.MethodGet, "/api/v1/agent/governance/snapshot"},
		{http.MethodGet, "/api/v1/agent/smoke"},
		{http.MethodGet, "/api/v1/agent/hermes/readiness"},
		{http.MethodGet, "/api/v1/agent/skills"},
		{http.MethodGet, "/api/v1/agent/schedules"},
		{http.MethodGet, "/api/v1/agent/executions"},
		{http.MethodPut, "/api/v1/agent/skills/x/actuation-level"},
		{http.MethodGet, "/api/v1/agent/nightly-report"},
		{http.MethodPost, "/api/v1/agent/nightly-run"},
		{http.MethodGet, "/api/v1/promote/release-cycles"},
		{http.MethodGet, "/api/v1/promote/release-cycles/x"},
		{http.MethodGet, "/api/v1/platform/escape-hatch"},
		{http.MethodPost, "/api/v1/platform/escape-hatch/drill"},
		{http.MethodGet, "/api/v1/stack/addons"},
		{http.MethodPost, "/api/v1/stack/addons/gitea/install"},
		{http.MethodPost, "/api/v1/stack/addons/gitea/upgrade"},
		{http.MethodPut, "/api/v1/agent/governance/trust-overrides/research-loop-batch"},
		{http.MethodGet, "/api/v1/remediation/health"},
		{http.MethodGet, "/api/v1/remediation/"},
		{http.MethodPost, "/api/v1/remediation/start"},
		{http.MethodGet, "/api/v1/remediation/job-1"},
		{http.MethodGet, "/api/v1/remediation/job-1/stream"},
		{http.MethodPost, "/api/v1/remediation/job-1/cancel"},
		{http.MethodPost, "/api/v1/remediation/job-1/respond"},
		{http.MethodGet, "/api/v1/agent/hermes/health"},
	}
	// The path still serves another method, so chi answers 405 rather than 404.
	methodGone := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/checklist/signals"},
		{http.MethodPost, "/api/v1/agent/deploy"},
	}
	for _, c := range retired {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404", c.method, c.path, rec.Code)
		}
	}
	for _, c := range methodGone {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s = %d, want 405", c.method, c.path, rec.Code)
		}
	}
}

func TestTrustMatrixMissingFileIs200(t *testing.T) {
	cfg := newTestConfig(t)
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	router := srv.Router()
	for _, path := range []string{
		"/api/v1/agent/governance/trust-matrix",
		"/api/v1/agent/governance/trust-overrides",
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s = %d, want 200; body %s", path, rec.Code, rec.Body.String())
			continue
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Errorf("%s decode: %v", path, err)
			continue
		}
		errText, _ := body["store_error"].(string)
		if errText == "" {
			t.Errorf("%s store_error is empty; body %s", path, rec.Body.String())
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

func TestPatrolReadsSharedStateInsteadOfProxy(t *testing.T) {
	var hits []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.URL.Path)
		http.NotFound(w, r)
	}))
	t.Cleanup(proxy.Close)
	t.Setenv("OPERATOR_PLANE_URL", proxy.URL)

	cfg := newTestConfig(t)
	stateDir := os.Getenv("PATROL_STATE_DIR")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"enabled":{},"runs":[{"id":"run-local","skill_id":"disk","result":"ok"}],"updated_at":"2026-10-08T00:00:00Z"}`)
	if err := os.WriteFile(filepath.Join(stateDir, "state.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	if srv.plane != nil {
		t.Fatal("proxying host constructed a local plane")
	}
	router := srv.Router()

	skills := httptest.NewRecorder()
	router.ServeHTTP(skills, httptest.NewRequest(http.MethodGet, "/api/v1/patrol/skills", nil))
	if skills.Code != http.StatusOK {
		t.Fatalf("GET /patrol/skills = %d, body %s", skills.Code, skills.Body.String())
	}
	runs := httptest.NewRecorder()
	router.ServeHTTP(runs, httptest.NewRequest(http.MethodGet, "/api/v1/patrol/runs", nil))
	if runs.Code != http.StatusOK || !strings.Contains(runs.Body.String(), "run-local") {
		t.Fatalf("GET /patrol/runs = %d, body %s", runs.Code, runs.Body.String())
	}
	for _, path := range hits {
		if strings.Contains(path, "/patrol/") {
			t.Fatalf("patrol request was proxied: %s", path)
		}
	}
}
