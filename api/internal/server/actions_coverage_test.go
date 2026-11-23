//bifrost:owner approvals

package server_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/server"
	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
)

// TestWriteRoutesAreCataloguedOrExempt walks the live router. A new write
// route fails until it is added to the catalog or to the exemption list.
func TestWriteRoutesAreCataloguedOrExempt(t *testing.T) {
	srv := newRouteServer(t)
	routes, ok := srv.Router().(chi.Routes)
	if !ok {
		t.Fatal("Router() is not a chi.Routes")
	}
	var mounted []actions.MountedRoute
	err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		mounted = append(mounted, actions.MountedRoute{Method: method, Pattern: route})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	problems, writes := actions.CheckCovered(mounted)
	if writes < 40 {
		t.Fatalf("walked only %d mutating routes: the walk is not seeing the router", writes)
	}
	for _, p := range problems {
		t.Error(p)
	}
}

func newRouteServer(t *testing.T) *server.Server {
	t.Helper()
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(filepath.Join(configDir, "programs"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"environments.yaml": "environments:\n  - id: dev\n    label: Dev\n    nginx_base: http://127.0.0.1:8080\n",
		"topology.yaml":     "deployment_phase: k3s_partial\nnodes:\n  - id: node-a\n    label: Node A\n    host: 10.0.0.1\n    group: linux\n    grid: { row: 1, col: 2 }\nedges: []\n",
		"clusters.yaml":     "clusters:\n  - id: test-cluster\n    label: Test Cluster\n    distribution: k3s\n    api_server: https://10.0.0.1:6443\n    node_ip: 10.0.0.1\n",
		"ops-context.yaml":  "meta:\n  version: \"v1\"\n  catalog_version: \"v1\"\ndeployment:\n  phase: k3s_partial\nfocus:\n  headline: \"test focus\"\nmilestones:\n  - id: m1\n    status: SIGNED\n",
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(configDir, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(configDir, "programs", "smoke-test.yaml"), []byte("id: smoke-test\ntitle: Smoke\nstatus: active\nphases:\n  - id: p1\n    title: Phase 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PLATFORM_CONFIG", filepath.Join(configDir, "environments.yaml"))
	t.Setenv("PLATFORM_DATA_DIR", filepath.Join(dir, "data"))
	t.Setenv("PLATFORM_AUTH_CONFIG", filepath.Join(dir, "does-not-exist-platform-auth.yaml"))
	t.Setenv("PLATFORM_REMEDIATION_JOBS_DIR", filepath.Join(dir, "remediation-jobs"))
	t.Setenv("PLATFORM_AUDIT_LOG", filepath.Join(dir, "audit.json"))
	t.Setenv("PLATFORM_KUBECONFIG", filepath.Join(dir, "does-not-exist-kubeconfig.yaml"))
	t.Setenv("PLATFORM_ROLE", "api")
	t.Setenv("PLATFORM_STATE_BACKEND", "file")
	// statefile.Use is process-wide. Clear a backend left by another test
	// in this package, and do not leave one behind.
	statefile.Use(nil, "")
	t.Cleanup(func() { statefile.Use(nil, "") })
	t.Setenv("PATROL_DISPATCH", "stub")
	t.Setenv("PATROL_STATE_DIR", filepath.Join(dir, "patrol-state"))
	t.Setenv("PATROL_SKILLS_DIR", filepath.Join(configDir, "patrol-skills"))
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	srv, err := server.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}
