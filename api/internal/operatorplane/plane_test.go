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
	"github.com/weitingzhao/bifrost-platform/api/internal/launchd"
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
		"/api/v1/agent/bridge",
		"/api/v1/agent/deploy",
		"/api/v1/agent/launchd",
	} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code == http.StatusNotFound {
			t.Errorf("%s is not routed by the plane", path)
		}
	}
}

// Patrol writes moved to platform-api. The plane must not serve them.
func TestPlaneDoesNotServePatrol(t *testing.T) {
	r := chi.NewRouter()
	r.Route("/api/v1", newPlane(t).Mount)

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/patrol/skills"},
		{http.MethodPut, "/api/v1/patrol/skills/x/enable"},
		{http.MethodPost, "/api/v1/patrol/trigger/x"},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s answered %d, want 404 from the plane", tc.method, tc.path, rec.Code)
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

// paths the plane answers, gathered by walking the router chi actually built.
func mountedRoutes(t *testing.T, mount func(chi.Router)) map[string]bool {
	t.Helper()
	r := chi.NewRouter()
	r.Route("/api/v1", mount)
	got := map[string]bool{}
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got[method+" "+route] = true
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return got
}

// The proxy exists so the Console keeps working when L-1 moves off-host. If the
// forwarded set ever differs from the served set, some route silently 404s in
// one topology and works in the other.
func TestProxyForwardsExactlyTheRoutesThePlaneServes(t *testing.T) {
	served := mountedRoutes(t, newPlane(t).Mount)

	mount, err := operatorplane.NewProxyMount(&actuation.AuthService{}, "http://127.0.0.1:8783")
	if err != nil {
		t.Fatalf("NewProxyMount: %v", err)
	}
	forwarded := mountedRoutes(t, mount)

	for route := range served {
		if !forwarded[route] {
			t.Errorf("%s is served in-process but not forwarded by the proxy", route)
		}
	}
	for route := range forwarded {
		if !served[route] {
			t.Errorf("%s is forwarded by the proxy but not served in-process", route)
		}
	}
}

func TestProxyMountRejectsAnUnusableURL(t *testing.T) {
	for _, target := range []string{"", "127.0.0.1:8783", "not a url", "/patrol"} {
		if _, err := operatorplane.NewProxyMount(&actuation.AuthService{}, target); err == nil {
			t.Errorf("NewProxyMount(%q) accepted an unusable operator plane URL", target)
		}
	}
}

// Patrol is not forwarded. A route the plane still serves fails closed at the
// proxy when the plane is down, instead of looking like platform-api is down.
func TestProxyDoesNotForwardPatrol(t *testing.T) {
	mount, err := operatorplane.NewProxyMount(&actuation.AuthService{}, "http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("NewProxyMount: %v", err)
	}
	r := chi.NewRouter()
	r.Route("/api/v1", mount)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/patrol/trigger/x", strings.NewReader("{}")))
	if rec.Code != http.StatusNotFound {
		t.Errorf("POST /patrol/trigger/x answered %d, want 404 (not proxied)", rec.Code)
	}
}

// An unreachable plane must not read as platform-api being broken.
func TestProxySaysWhichHalfIsDown(t *testing.T) {
	mount, err := operatorplane.NewProxyMount(&actuation.AuthService{}, "http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("NewProxyMount: %v", err)
	}
	r := chi.NewRouter()
	r.Route("/api/v1", mount)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/agent/bridge", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("unreachable plane answered %d, want 502", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "operator plane unreachable") {
		t.Errorf("502 body does not name the operator plane: %s", body)
	}
}

func TestLaunchdRequiresViewerToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PLATFORM_DATA_DIR", filepath.Join(dir, "data"))
	if err := os.MkdirAll(filepath.Join(dir, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	authPath := filepath.Join(dir, "platform-auth.yaml")
	if err := os.WriteFile(authPath, []byte("tokens:\n  - name: viewer\n    role: viewer\n    token: viewer-token-e1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	auth, err := actuation.LoadAuth(authPath)
	if err != nil {
		t.Fatal(err)
	}
	p, err := operatorplane.New(operatorplane.Deps{
		Auth:      auth,
		Audit:     actuation.NewAuditLog(""),
		ConfigDir: filepath.Join(dir, "config"),
		// CI runs on Linux, where launchctl does not exist.
		ListLaunchd: func() ([]launchd.Service, error) {
			return []launchd.Service{{Label: "com.bifrost.operator-plane", PID: 42, Running: true, Plist: true}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.StopBackground)
	r := chi.NewRouter()
	r.Route("/api/v1", p.Mount)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/agent/launchd", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("launchd without a token answered %d, want 401", rec.Code)
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/launchd", nil)
	req.Header.Set("Authorization", "Bearer viewer-token-e1")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer launchd answered %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"label":"com.bifrost.operator-plane"`) {
		t.Fatalf("body: %s", rec.Body.String())
	}

	mount, err := operatorplane.NewProxyMount(auth, "http://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	proxy := chi.NewRouter()
	proxy.Route("/api/v1", mount)
	rec = httptest.NewRecorder()
	proxy.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/agent/launchd", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("proxy launchd without a token answered %d, want 401 before the hop", rec.Code)
	}
}
