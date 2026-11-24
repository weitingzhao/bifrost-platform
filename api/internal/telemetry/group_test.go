package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

// testConfigWithCluster is a config with one cluster; the Prometheus address
// comes from PLATFORM_PROMETHEUS_URL.
func testConfigWithCluster(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{Clusters: &config.ClustersFile{Clusters: []config.ClusterEntry{{ID: "test"}}}}
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

// groupQueries is a registered group of the shape the compute domain
// registers (2026-11-22: the compute readings moved to computereadings; the
// service serves whatever group a domain registered).
func groupQueries() []QuerySpec {
	return []QuerySpec{
		{ID: "g_one", Title: "one", Unit: "n", Target: "t1", Note: "n1", Build: func(string) string { return "node_pressure_one" }},
		{ID: "g_two", Title: "two", Unit: "n", Target: "t2", Note: "n2", Build: func(string) string { return "node_load1" }},
	}
}

// Origin: 2026-11-20 the compute group (law 97); 2026-11-22 the group moved to
// the compute domain and telemetry serves what is registered: a registered
// group runs only its readings (control: TestOverviewWithoutAGroupIsUnchanged).
func TestOverviewARegisteredGroupRunsOnlyItsReadings(t *testing.T) {
	var asked []string
	srv := promStub(t, &asked)
	defer srv.Close()
	t.Setenv("PLATFORM_PROMETHEUS_URL", srv.URL)
	svc := NewService(testConfigWithCluster(t))
	svc.SetGroup("g", groupQueries)

	resp, err := svc.Overview(context.Background(), "", "g")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Group != "g" || len(resp.Metrics) != len(groupQueries()) || len(asked) != len(groupQueries()) {
		t.Fatalf("group %q, %d metrics, %d queries asked; want %d", resp.Group, len(resp.Metrics), len(asked), len(groupQueries()))
	}
	for i, m := range resp.Metrics {
		want := groupQueries()[i]
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
	svc.SetGroup("g", groupQueries)

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

// Origin: 2026-11-22 17:5xZ, the compute line measured 7 of the 26 compute
// readings timing out on PROD because all 26 went to Prometheus at once and
// queued behind its 20 concurrent queries; alone each took 1-2.2 s. An
// overview sends at most maxInFlight queries at a time and still answers all
// of them; the control is that it does run them in parallel (more than one
// at once), so the bound is not a serial loop.
func TestOverviewSendsAtMostMaxInFlightQueriesAtOnce(t *testing.T) {
	var mu sync.Mutex
	inFlight, peak, asked := 0, 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		inFlight++
		asked++
		if inFlight > peak {
			peak = inFlight
		}
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "vector", "result": []any{}}})
	}))
	defer srv.Close()
	t.Setenv("PLATFORM_PROMETHEUS_URL", srv.URL)
	svc := NewService(testConfigWithCluster(t))
	var many []QuerySpec
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("q%d", i)
		many = append(many, QuerySpec{ID: id, Title: id, Unit: "n", Build: func(string) string { return "up" }})
	}
	svc.SetGroup("many", func() []QuerySpec { return many })
	resp, err := svc.Overview(context.Background(), "", "many")
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Metrics) != 20 || asked != 20 {
		t.Fatalf("%d metrics, %d asked; want 20", len(resp.Metrics), asked)
	}
	if peak > maxInFlight || peak < 2 {
		t.Fatalf("peak %d queries at once; want between 2 and %d", peak, maxInFlight)
	}
}
