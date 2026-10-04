package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

func TestTradeRoutesEightPrefixesFourProcesses(t *testing.T) {
	if n := len(TradeGatewayRoutes()); n != 8 {
		t.Fatalf("routes = %d, want 8", n)
	}
	want := []string{"api-monitor", "api-account", "api-market", "api-research"}
	got := TradeProcesses()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("processes = %v, want %v", got, want)
	}
	ops, _ := TradeRouteFor("ops")
	if ops.GatewayPath() != "/api/ops/ops/health" || ops.Process != "api-monitor" {
		t.Fatalf("ops route = %+v (want /api/ops/ops/health on api-monitor)", ops)
	}
}

// config/trade-api-domains.yaml is the registry agents read; it must say what the probes do.
func TestTradeRoutesMatchRegistry(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "config", "trade-api-domains.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var reg struct {
		Domains []struct {
			ID        string `yaml:"id"`
			Port      int    `yaml:"port"`
			ProbePath string `yaml:"probe_path"`
			Process   string `yaml:"process"`
			Service   string `yaml:"service"`
		} `yaml:"domains"`
	}
	if err := yaml.Unmarshal(raw, &reg); err != nil {
		t.Fatal(err)
	}
	routes := TradeGatewayRoutes()
	if len(reg.Domains) != len(routes) {
		t.Fatalf("registry has %d domains, catalog %d", len(reg.Domains), len(routes))
	}
	for i, r := range routes {
		d := reg.Domains[i]
		if d.ID != r.Prefix || d.Port != r.Port || d.ProbePath != r.ProbePath || d.Process != r.Process || d.Service != r.Service {
			t.Fatalf("domain %d: registry %+v, catalog %+v", i, d, r)
		}
	}
}

func TestCheckAnswer(t *testing.T) {
	ops, _ := TradeRouteFor("ops")
	monitor, _ := TradeRouteFor("monitor")
	cases := []struct {
		name  string
		route TradeGatewayRoute
		ct    string
		body  string
		ok    bool
	}{
		{"right service", ops, "application/json", `{"status":"ok","service":"bifrost-ops"}`, true},
		{"monitor's generic health under /api/ops", ops, "application/json", `{"status":"ok","service":"bifrost-monitor"}`, false},
		{"SPA fallback", ops, "text/html; charset=utf-8", `<!doctype html><html></html>`, false},
		{"HTML without a content type", ops, "", `<!DOCTYPE html>`, false},
		{"not JSON", ops, "application/json", `ok`, false},
		{"status has no service field", monitor, "application/json", `{"daemon":{}}`, true},
	}
	for _, tc := range cases {
		ok, detail := tc.route.CheckAnswer(tc.ct, []byte(tc.body))
		if ok != tc.ok {
			t.Fatalf("%s: ok=%v want %v (%s)", tc.name, ok, tc.ok, detail)
		}
	}
}

func TestProbeTradeRoute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ops/ops/health":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok","service":"bifrost-ops"}`))
		case "/api/research/health":
			// A route the gateway lost: the SPA answers.
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<!doctype html><title>Bifrost</title>`))
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()

	p := NewProber()
	ops, _ := TradeRouteFor("ops")
	got := p.probeTradeRoute(context.Background(), ops, srv.URL, config.Environment{})
	if got.Reachability != ReachOK || got.ID != "api-ops" || got.Process != "api-monitor" {
		t.Fatalf("ops: %+v", got)
	}
	research, _ := TradeRouteFor("research")
	got = p.probeTradeRoute(context.Background(), research, srv.URL, config.Environment{})
	if got.Reachability != ReachFail || !strings.Contains(got.Detail, "SPA fallback") {
		t.Fatalf("research via SPA: %+v", got)
	}
	monitor, _ := TradeRouteFor("monitor")
	got = p.probeTradeRoute(context.Background(), monitor, srv.URL, config.Environment{})
	if got.Reachability != ReachDegraded {
		t.Fatalf("monitor 503: %+v", got)
	}
}
