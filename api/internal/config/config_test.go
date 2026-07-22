package config

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const fixtureEnvironmentsYAML = `
environments:
  - id: dev
    label: Dev
    probe_mode: pull
    nginx_base: http://127.0.0.1:8080
    ingress_host: dev.bifrost.lan
    ingress_node_ip: 127.0.0.1
    postgres:
      host: 127.0.0.1
      port: 5432
    redis:
      host: 127.0.0.1
      port: 6379
    ops_token_env: TEST_DEV_OPS_TOKEN
  - id: prod
    label: Prod
    nginx_base: http://192.168.10.70
`

const fixtureTopologyYAML = `
deployment_phase: k3s_partial
nodes:
  - id: node-a
    label: Node A
    host: 10.0.0.1
    group: linux
    compose_roles: [nginx]
    k3s_roles: [k3s_server_1]
    in_k3s_cluster: true
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
    bifrost_namespaces: [bifrost-dev]
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

// writeFixtureConfigDir writes the four config files config.Load needs into a
// temp directory and returns the path to environments.yaml (the entry point).
func writeFixtureConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"environments.yaml": fixtureEnvironmentsYAML,
		"topology.yaml":     fixtureTopologyYAML,
		"clusters.yaml":     fixtureClustersYAML,
		"ops-context.yaml":  fixtureOpsContextYAML,
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return filepath.Join(dir, "environments.yaml")
}

func TestLoadReadsFullConfigTree(t *testing.T) {
	configPath := writeFixtureConfigDir(t)
	t.Setenv("PLATFORM_CONFIG", configPath)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Environments) != 2 {
		t.Fatalf("Environments = %d, want 2", len(cfg.Environments))
	}
	if cfg.Listen != ":8780" {
		t.Fatalf("Listen = %q, want default :8780", cfg.Listen)
	}
	if cfg.Topology == nil || len(cfg.Topology.Nodes) != 1 {
		t.Fatalf("Topology = %+v", cfg.Topology)
	}
	if cfg.Clusters == nil || len(cfg.Clusters.Clusters) != 1 {
		t.Fatalf("Clusters = %+v", cfg.Clusters)
	}
	if cfg.OpsContext == nil || cfg.OpsContext.Focus.Headline != "test focus" {
		t.Fatalf("OpsContext = %+v", cfg.OpsContext)
	}
	if cfg.ConfigDir() != filepath.Dir(configPath) {
		t.Fatalf("ConfigDir() = %q, want %q", cfg.ConfigDir(), filepath.Dir(configPath))
	}
}

func TestLoadRespectsListenOverride(t *testing.T) {
	configPath := writeFixtureConfigDir(t)
	t.Setenv("PLATFORM_CONFIG", configPath)
	t.Setenv("PLATFORM_LISTEN", ":9999")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Listen != ":9999" {
		t.Fatalf("Listen = %q, want :9999", cfg.Listen)
	}
}

func TestLoadMissingFileReturnsError(t *testing.T) {
	t.Setenv("PLATFORM_CONFIG", filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if _, err := Load(); err == nil {
		t.Fatal("expected error for missing config file")
	}
}

func TestLoadRejectsEmptyEnvironments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "environments.yaml")
	if err := os.WriteFile(path, []byte("environments: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PLATFORM_CONFIG", path)
	if _, err := Load(); err == nil {
		t.Fatal("expected error for empty environments list")
	}
}

func TestGetEnvironment(t *testing.T) {
	cfg := &Config{Environments: []Environment{
		{ID: "dev", Label: "Dev"},
		{ID: "prod", Label: "Prod"},
	}}
	env, ok := cfg.GetEnvironment("prod")
	if !ok || env.Label != "Prod" {
		t.Fatalf("GetEnvironment(prod) = %+v, %v", env, ok)
	}
	if _, ok := cfg.GetEnvironment("missing"); ok {
		t.Fatal("expected GetEnvironment(missing) to report not found")
	}
}

func TestReloadOpsContext(t *testing.T) {
	configPath := writeFixtureConfigDir(t)
	t.Setenv("PLATFORM_CONFIG", configPath)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Mutate the on-disk ops-context.yaml and confirm ReloadOpsContext picks it up.
	if err := os.WriteFile(cfg.OpsContextPath, []byte(`
meta:
  version: "v2"
  catalog_version: "v2"
deployment:
  phase: k3s_ha
focus:
  headline: "updated focus"
milestones:
  - id: m1
    status: CLOSED
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cfg.ReloadOpsContext(); err != nil {
		t.Fatalf("ReloadOpsContext: %v", err)
	}
	if cfg.OpsContext.Focus.Headline != "updated focus" {
		t.Fatalf("OpsContext.Focus.Headline = %q, want updated focus", cfg.OpsContext.Focus.Headline)
	}
}

func TestEnvironmentOpsToken(t *testing.T) {
	env := &Environment{OpsTokenEnv: "TEST_OPS_TOKEN_XYZ"}
	if got := env.OpsToken(); got != "" {
		t.Fatalf("OpsToken() = %q before env set, want empty", got)
	}
	t.Setenv("TEST_OPS_TOKEN_XYZ", "secret-token")
	if got := env.OpsToken(); got != "secret-token" {
		t.Fatalf("OpsToken() = %q, want secret-token", got)
	}

	noEnv := &Environment{}
	if got := noEnv.OpsToken(); got != "" {
		t.Fatalf("OpsToken() with no ops_token_env = %q, want empty", got)
	}
}

func TestEnvironmentEffectiveProbeMode(t *testing.T) {
	cases := []struct {
		env  *Environment
		want string
	}{
		{nil, "pull"},
		{&Environment{}, "pull"},
		{&Environment{ProbeMode: "bridge"}, "bridge"},
		{&Environment{ProbeMode: "BRIDGE"}, "bridge"},
		{&Environment{ProbeMode: "pull"}, "pull"},
		{&Environment{ProbeMode: "unknown"}, "pull"},
	}
	for _, tc := range cases {
		if got := tc.env.EffectiveProbeMode(); got != tc.want {
			t.Fatalf("EffectiveProbeMode(%+v) = %q, want %q", tc.env, got, tc.want)
		}
	}
}

func TestEnvironmentEffectiveTradeBridgeURL(t *testing.T) {
	t.Setenv("SATELLITE_PROBE_BRIDGE_URL", "http://fallback:8786")

	envWithOverride := &Environment{TradeBridgeURL: "http://override:8786/"}
	if got := envWithOverride.EffectiveTradeBridgeURL(); got != "http://override:8786" {
		t.Fatalf("EffectiveTradeBridgeURL() = %q, want override without trailing slash", got)
	}

	envWithoutOverride := &Environment{}
	if got := envWithoutOverride.EffectiveTradeBridgeURL(); got != "http://fallback:8786" {
		t.Fatalf("EffectiveTradeBridgeURL() = %q, want fallback", got)
	}

	if got := (*Environment)(nil).EffectiveTradeBridgeURL(); got != "http://fallback:8786" {
		t.Fatalf("EffectiveTradeBridgeURL(nil) = %q, want fallback", got)
	}
}

func TestNeedsTraefikHostHeader(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"", false},
		{"http://10.0.0.1:30880", false},
		{"http://10.0.0.1:30881", false},
		{"http://10.0.0.1:30882", false},
		{"http://192.168.10.70", false},
		{"http://192.168.10.70:8080/x", false},
		{"http://192.168.10.100", true},
		{"http://some-other-host", true},
	}
	for _, tc := range cases {
		if got := NeedsTraefikHostHeader(tc.url); got != tc.want {
			t.Fatalf("NeedsTraefikHostHeader(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}

func TestApplyIngressHost(t *testing.T) {
	env := &Environment{NginxBase: "http://192.168.10.100", IngressHost: "trader.bifrost.lan"}
	req := httptest.NewRequest(http.MethodGet, "http://192.168.10.100/api/health", nil)
	env.ApplyIngressHost(req)
	if req.Host != "trader.bifrost.lan" {
		t.Fatalf("req.Host = %q, want trader.bifrost.lan", req.Host)
	}
	if req.Header.Get("Host") != "trader.bifrost.lan" {
		t.Fatalf("Host header = %q, want trader.bifrost.lan", req.Header.Get("Host"))
	}

	// NodePort gateway does not need the Host header rewrite.
	nodePortEnv := &Environment{NginxBase: "http://10.0.0.1:30880", IngressHost: "should-not-apply"}
	req2 := httptest.NewRequest(http.MethodGet, "http://10.0.0.1:30880/api/health", nil)
	nodePortEnv.ApplyIngressHost(req2)
	if req2.Host == "should-not-apply" {
		t.Fatal("ApplyIngressHost should not rewrite Host for NodePort gateways")
	}

	// nil env / nil request must be no-ops, not panics.
	(*Environment)(nil).ApplyIngressHost(req)
	env.ApplyIngressHost(nil)
}
