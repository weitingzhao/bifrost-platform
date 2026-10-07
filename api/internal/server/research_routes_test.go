package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestResearchRoutesAreReadOnly is the TD-190 ratchet. Platform used to expose
// POST /research/cronjobs/{name}/trigger, which built a Job from a whitelist of
// suspended research CronJob templates and kept those templates alive. Research
// batch work is Dagster's: platform only reads research status and proxies GETs.
// Any non-GET route under /research or /plugins/research fails this test.
func TestResearchRoutesAreReadOnly(t *testing.T) {
	cfg := newTestConfig(t)
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	routes, ok := srv.Router().(chi.Routes)
	if !ok {
		t.Fatalf("Router() is %T, want chi.Routes", srv.Router())
	}
	seen := 0
	walk := func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/api/v1/research/") && !strings.HasPrefix(route, "/api/v1/plugins/research/") {
			return nil
		}
		seen++
		if method != http.MethodGet && method != http.MethodHead {
			t.Errorf("research route %s %s: platform must not write to research (TD-190)", method, route)
		}
		if strings.Contains(route, "cronjob") {
			t.Errorf("research route %s %s: the CronJob trigger was removed (TD-190)", method, route)
		}
		return nil
	}
	if err := chi.Walk(routes, walk); err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	if seen == 0 {
		t.Fatal("no /api/v1/research routes found; the walk is not looking at the real router")
	}
}
