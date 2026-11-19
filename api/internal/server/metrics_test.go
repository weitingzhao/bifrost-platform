//bifrost:owner foundation

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/maintainer"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

func TestWorkersMetricsDoesNotProbePlugins(t *testing.T) {
	var calls atomic.Int32
	fake := func(context.Context) probe.PluginHealth {
		calls.Add(1)
		return probe.PluginHealth{Name: "fake", Reachable: true, Reachability: probe.ReachOK}
	}
	srv := &Server{
		authLoaded:  true,
		httpMetrics: newHTTPMetrics(),
		pluginProbes: []func(context.Context) probe.PluginHealth{
			fake, fake, fake, fake,
		},
	}
	maintainer.Reset()
	t.Cleanup(maintainer.Reset)
	maintainer.Success("platform/prod/ib-autorepair")

	scrape := func() string {
		t.Helper()
		rec := httptest.NewRecorder()
		srv.handleMetrics(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
		return rec.Body.String()
	}

	t.Setenv(config.RoleEnv, string(config.RoleWorkers))
	body := scrape()
	if n := calls.Load(); n != 0 {
		t.Fatalf("workers /metrics called plugin probes %d times", n)
	}
	if strings.Contains(body, "bifrost_plugin_reachable") {
		t.Fatalf("workers /metrics exported plugin reachability:\n%s", body)
	}
	for _, want := range []string{
		`bifrost_maintainer_last_success_timestamp_seconds{maintainer="platform/prod/ib-autorepair"}`,
		"bifrost_maintainer_runs_total",
		"bifrost_platform_auth_loaded 1",
		"# TYPE http_requests_total counter",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in workers /metrics:\n%s", want, body)
		}
	}

	// The api role still probes. The same fakes must run, or a workers skip
	// that never consults them would not prove the counter is wired.
	t.Setenv(config.RoleEnv, string(config.RoleAPI))
	body = scrape()
	if n := calls.Load(); n != 4 {
		t.Fatalf("api /metrics called plugin probes %d times, want 4", n)
	}
	if !strings.Contains(body, `bifrost_plugin_reachable{plugin="fake"} 1`) {
		t.Fatalf("api /metrics dropped plugin reachability:\n%s", body)
	}
}
