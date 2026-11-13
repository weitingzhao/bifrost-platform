package telemetry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

// testConfigWithCluster is a config with one cluster; the Prometheus address
// comes from PLATFORM_PROMETHEUS_URL.
func testConfigWithCluster(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{Clusters: &config.ClustersFile{Clusters: []config.ClusterEntry{{ID: "test"}}}}
}

func TestComputeQueriesAreNamedOnceAndSayWhatTheyAnswer(t *testing.T) {
	seen := map[string]bool{}
	for _, q := range PresetQueries() {
		seen[q.ID] = true
	}
	for _, q := range ComputeQueries() {
		if seen[q.ID] {
			t.Errorf("%s is defined twice", q.ID)
		}
		seen[q.ID] = true
		if !strings.HasPrefix(q.ID, "compute_") || q.Title == "" || q.Unit == "" || q.Target == "" || q.Note == "" {
			t.Errorf("%s: id prefix, title, unit, target and note are all required: %+v", q.ID, q)
		}
		if expr := q.Build("ignored"); !strings.Contains(expr, computeWindow) && q.ID != "compute_pods_evicted" {
			t.Errorf("%s does not look back %s: %s", q.ID, computeWindow, expr)
		}
		if _, ok := FindQuery(q.ID); !ok {
			t.Errorf("FindQuery does not know %s", q.ID)
		}
	}
}

// promStub answers every instant query with one sample and records the
// expressions it was asked. The queries of one overview arrive at once, so the
// record is guarded.
func promStub(t *testing.T, asked *[]string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		mu.Lock()
		*asked = append(*asked, r.Form.Get("query"))
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{"resultType": "vector", "result": []any{
				map[string]any{"metric": map[string]string{"nodename": "ubt-k3s-06"}, "value": []any{1.0, "5.5"}},
			}},
		})
	}))
}

func TestOverviewGroupComputeRunsOnlyTheComputeReadings(t *testing.T) {
	var asked []string
	srv := promStub(t, &asked)
	defer srv.Close()
	t.Setenv("PLATFORM_PROMETHEUS_URL", srv.URL)
	svc := NewService(testConfigWithCluster(t))

	resp, err := svc.Overview(context.Background(), "", GroupCompute)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Group != GroupCompute || len(resp.Metrics) != len(ComputeQueries()) || len(asked) != len(ComputeQueries()) {
		t.Fatalf("group %q, %d metrics, %d queries asked; want %d", resp.Group, len(resp.Metrics), len(asked), len(ComputeQueries()))
	}
	for i, m := range resp.Metrics {
		want := ComputeQueries()[i]
		if m.ID != want.ID || m.Target != want.Target || m.Note != want.Note || m.Status != "ok" || m.Points[0].Value != 5.5 {
			t.Errorf("metric %d = %+v, want id %s with its target and note", i, m, want.ID)
		}
	}
}

func TestOverviewWithoutAGroupIsUnchanged(t *testing.T) {
	var asked []string
	srv := promStub(t, &asked)
	defer srv.Close()
	t.Setenv("PLATFORM_PROMETHEUS_URL", srv.URL)
	svc := NewService(testConfigWithCluster(t))

	for _, group := range []string{"", "nonsense"} {
		asked = nil
		resp, err := svc.Overview(context.Background(), "", group)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Group != "" || len(resp.Metrics) != len(PresetQueries()) {
			t.Fatalf("group %q: got group %q and %d metrics, want the %d presets", group, resp.Group, len(resp.Metrics), len(PresetQueries()))
		}
		for _, q := range asked {
			if strings.Contains(q, "node_pressure") || strings.Contains(q, "node_load1") {
				t.Fatalf("group %q ran a compute query: %s", group, q)
			}
		}
		for _, m := range resp.Metrics {
			if m.Target != "" || m.Note != "" {
				t.Errorf("preset %s gained a target or note", m.ID)
			}
		}
	}
}
