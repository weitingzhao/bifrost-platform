package operatorplane_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/operatorplane"
)

func newPlane(t *testing.T) *operatorplane.Plane {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PLATFORM_DATA_DIR", filepath.Join(dir, "data"))
	if err := os.MkdirAll(filepath.Join(dir, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := operatorplane.New(operatorplane.Deps{
		Auth:      &actuation.AuthService{},
		Audit:     actuation.NewAuditLog(""),
		ConfigDir: filepath.Join(dir, "config"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(p.StopBackground)
	return p
}

// The plane must serve the same paths whether it is mounted inside platform-api
// or run on its own — that equivalence is the whole point of extracting it.
func TestMountServesEveryOperatorPlaneRoute(t *testing.T) {
	r := chi.NewRouter()
	r.Route("/api/v1", newPlane(t).Mount)

	for _, path := range []string{
		"/api/v1/agent/nightly-report",
		"/api/v1/agent/bridge",
		"/api/v1/agent/smoke",
		"/api/v1/agent/deploy",
		"/api/v1/agent/hermes/readiness",
		"/api/v1/agent/hermes/first-task",
		"/api/v1/agent/hermes/health",
		"/api/v1/agent/skills",
		"/api/v1/agent/schedules",
		"/api/v1/agent/executions",
		"/api/v1/hermes/insights",
		"/api/v1/patrol/skills",
		"/api/v1/patrol/runs",
		"/api/v1/agent/drift-proposals/",
	} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code == http.StatusNotFound {
			t.Errorf("%s is not routed by the plane", path)
		}
	}
}

// Reading is viewer level; anything that actuates a runner, a deploy or a skill
// stays operator gated after the move.
func TestActuationRoutesStayOperatorGated(t *testing.T) {
	r := chi.NewRouter()
	r.Route("/api/v1", newPlane(t).Mount)

	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/agent/nightly-run"},
		{http.MethodPost, "/api/v1/agent/deploy"},
		{http.MethodPut, "/api/v1/patrol/skills/x/enable"},
		{http.MethodPost, "/api/v1/patrol/trigger/x"},
		{http.MethodPost, "/api/v1/agent/drift-proposals/"},
		{http.MethodPost, "/api/v1/agent/drift-proposals/x/approve"},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusForbidden {
			t.Errorf("%s %s answered %d without operator auth, want 401/403", tc.method, tc.path, rec.Code)
		}
	}
}

// The autopilot loop belongs to exactly one process. Whoever mounts the plane
// decides; mounting alone must not start it.
func TestMountDoesNotStartTheAutopilot(t *testing.T) {
	p := newPlane(t)
	r := chi.NewRouter()
	r.Route("/api/v1", p.Mount)
	if p.Patrol().Running() {
		t.Fatal("mounting the plane must not start the patrol loop")
	}
	p.StartBackground(t.Context())
	if !p.Patrol().Running() {
		t.Fatal("StartBackground must start the patrol loop")
	}
	p.StopBackground()
	if p.Patrol().Running() {
		t.Fatal("StopBackground must stop the patrol loop")
	}
}
