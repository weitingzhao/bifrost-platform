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

func TestTradeRoutesOnePrefixPerProcess(t *testing.T) {
	if n := len(TradeGatewayRoutes()); n != 6 {
		t.Fatalf("routes = %d, want 6", n)
	}
	want := []string{"api-monitor", "api-account", "api-market", "api-research"}
	got := TradeProcesses()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("processes = %v, want %v", got, want)
	}
	// TD-55 option B: each process has exactly one prefix, named after it.
	prefixOf := map[string]string{}
	for _, r := range TradeGatewayRoutes() {
		if p, ok := prefixOf[r.Process]; ok && p != r.Prefix {
			t.Fatalf("%s answers at two prefixes: %s and %s", r.Process, p, r.Prefix)
		}
		prefixOf[r.Process] = r.Prefix
		if "api-"+r.Prefix != r.Process {
			t.Fatalf("route %s: prefix %q is not its process %q", r.ID, r.Prefix, r.Process)
		}
	}
	ops, _ := TradeRouteFor("ops")
	if ops.GatewayPath() != "/api/monitor/ops/health" || ops.Process != "api-monitor" || ops.TargetID() != "api-ops" {
		t.Fatalf("ops route = %+v (want /api/monitor/ops/health on api-monitor, target api-ops)", ops)
	}
}

// Each mapping: route id or alias prefix → the gateway path the platform asks for.
func TestTradeRouteForMapsEveryNameToItsProcessPrefix(t *testing.T) {
	cases := map[string]string{
		"monitor":   "/api/monitor/status",
		"docs":      "/api/monitor/research/docs/health",
		"ops":       "/api/monitor/ops/health",
		"account":   "/api/account/health",
		"market":    "/api/market/health",
		"research":  "/api/research/health",
		"trading":   "/api/account/health",
		"strategy":  "/api/account/health",
		"portfolio": "/api/account/health",
	}
	for name, want := range cases {
		r, ok := TradeRouteFor(name)
		if !ok || r.GatewayPath() != want {
			t.Fatalf("TradeRouteFor(%q) = %+v %v, want path %s", name, r, ok, want)
		}
	}
	if _, ok := TradeRouteFor("nope"); ok {
		t.Fatal("unknown name resolved")
	}
	if got := TradeGatewayPath("ops", "/ops/data-probe"); got != "/api/monitor/ops/data-probe" {
		t.Fatalf("TradeGatewayPath = %s", got)
	}
}

// B2 retires the alias prefixes after 7 days of zero Traefik traffic, so no probe may use one.
func TestNoProbeUsesAnAliasPrefix(t *testing.T) {
	aliases := map[string]bool{}
	for _, a := range TradeGatewayAliases() {
		aliases[a.Prefix] = true
		r, ok := TradeRouteFor(a.Use)
		if !ok || r.Process != a.Process {
			t.Fatalf("alias %s → %s: replacement %+v is not on %s", a.Prefix, a.Use, r, a.Process)
		}
	}
	if len(aliases) != 5 {
		t.Fatalf("aliases = %v, want docs, ops, trading, strategy, portfolio", aliases)
	}
	for _, r := range TradeGatewayRoutes() {
		if aliases[r.Prefix] {
			t.Fatalf("route %s probes alias prefix /api/%s", r.ID, r.Prefix)
		}
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
			Prefix    string `yaml:"prefix"`
			Port      int    `yaml:"port"`
			ProbePath string `yaml:"probe_path"`
			Process   string `yaml:"process"`
			Service   string `yaml:"service"`
		} `yaml:"domains"`
		Aliases []struct {
			Prefix  string `yaml:"prefix"`
			Process string `yaml:"process"`
			Use     string `yaml:"use"`
		} `yaml:"aliases"`
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
		if d.ID != r.ID || d.Prefix != r.Prefix || d.Port != r.Port || d.ProbePath != r.ProbePath || d.Process != r.Process || d.Service != r.Service {
			t.Fatalf("domain %d: registry %+v, catalog %+v", i, d, r)
		}
	}
	aliases := TradeGatewayAliases()
	if len(reg.Aliases) != len(aliases) {
		t.Fatalf("registry has %d aliases, catalog %d", len(reg.Aliases), len(aliases))
	}
	for i, a := range aliases {
		d := reg.Aliases[i]
		if d.Prefix != a.Prefix || d.Process != a.Process || d.Use != a.Use {
			t.Fatalf("alias %d: registry %+v, catalog %+v", i, d, a)
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
		case "/api/monitor/ops/health":
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
