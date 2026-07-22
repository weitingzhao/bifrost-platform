package config

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func writeFixtureClustersFile(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "clusters.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadClustersSuccess(t *testing.T) {
	dir := writeFixtureClustersFile(t, fixtureClustersYAML)
	file, path, err := LoadClusters(dir)
	if err != nil {
		t.Fatalf("LoadClusters: %v", err)
	}
	if len(file.Clusters) != 1 || file.Clusters[0].ID != "test-cluster" {
		t.Fatalf("Clusters = %+v", file.Clusters)
	}
	if path != filepath.Join(dir, "clusters.yaml") {
		t.Fatalf("path = %q", path)
	}
}

func TestLoadClustersMissingFile(t *testing.T) {
	if _, _, err := LoadClusters(t.TempDir()); err == nil {
		t.Fatal("expected error for missing clusters.yaml")
	}
}

func TestLoadClustersEmptyList(t *testing.T) {
	dir := writeFixtureClustersFile(t, "clusters: []\n")
	if _, _, err := LoadClusters(dir); err == nil {
		t.Fatal("expected error for empty clusters list")
	}
}

func TestLoadClustersEnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom-clusters.yaml")
	if err := os.WriteFile(path, []byte(fixtureClustersYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PLATFORM_CLUSTERS", path)
	file, gotPath, err := LoadClusters("/some/other/dir")
	if err != nil {
		t.Fatalf("LoadClusters: %v", err)
	}
	if gotPath != path {
		t.Fatalf("path = %q, want env override %q", gotPath, path)
	}
	if len(file.Clusters) != 1 {
		t.Fatalf("Clusters = %+v", file.Clusters)
	}
}

func TestDefaultCluster(t *testing.T) {
	cfg := &Config{}
	if cfg.DefaultCluster() != nil {
		t.Fatal("expected nil DefaultCluster with no clusters loaded")
	}
	cfg.Clusters = &ClustersFile{Clusters: []ClusterEntry{
		{ID: "first"}, {ID: "second"},
	}}
	if got := cfg.DefaultCluster(); got == nil || got.ID != "first" {
		t.Fatalf("DefaultCluster() = %+v, want first entry", got)
	}
}

func TestKubeconfigPath(t *testing.T) {
	entry := &ClusterEntry{KubeconfigEnv: "TEST_KUBECONFIG_PATH"}
	t.Setenv("TEST_KUBECONFIG_PATH", "/custom/kubeconfig.yaml")
	if got := entry.KubeconfigPath(); got != "/custom/kubeconfig.yaml" {
		t.Fatalf("KubeconfigPath() = %q, want override", got)
	}

	fallback := &ClusterEntry{}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".kube", "bifrost-k3s.yaml")
	if got := fallback.KubeconfigPath(); got != want {
		t.Fatalf("KubeconfigPath() default = %q, want %q", got, want)
	}
}

func TestResolvedMonitoringNamespace(t *testing.T) {
	if got := (*ClusterEntry)(nil).ResolvedMonitoringNamespace(); got != "monitoring" {
		t.Fatalf("nil entry ResolvedMonitoringNamespace() = %q", got)
	}
	if got := (&ClusterEntry{}).ResolvedMonitoringNamespace(); got != "monitoring" {
		t.Fatalf("empty entry ResolvedMonitoringNamespace() = %q", got)
	}
	if got := (&ClusterEntry{MonitoringNS: "custom-ns"}).ResolvedMonitoringNamespace(); got != "custom-ns" {
		t.Fatalf("ResolvedMonitoringNamespace() = %q, want custom-ns", got)
	}
}

func TestObservabilityURLs(t *testing.T) {
	entry := &ClusterEntry{ObservabilityURLs: ObservabilityURLs{
		Grafana: "http://grafana.internal", Prometheus: "http://prom.internal", DocsInfra: "http://docs.internal",
	}}
	if got := entry.GrafanaURL(); got != "http://grafana.internal" {
		t.Fatalf("GrafanaURL() = %q", got)
	}
	if got := entry.PrometheusURL(); got != "http://prom.internal" {
		t.Fatalf("PrometheusURL() = %q", got)
	}
	if got := entry.ObservabilityDocsURL(); got != "http://docs.internal" {
		t.Fatalf("ObservabilityDocsURL() = %q", got)
	}

	t.Setenv("PLATFORM_GRAFANA_URL", "http://grafana.override")
	if got := entry.GrafanaURL(); got != "http://grafana.override" {
		t.Fatalf("GrafanaURL() override = %q", got)
	}
	t.Setenv("PLATFORM_PROMETHEUS_URL", "http://prom.override")
	if got := entry.PrometheusURL(); got != "http://prom.override" {
		t.Fatalf("PrometheusURL() override = %q", got)
	}

	if got := (*ClusterEntry)(nil).ObservabilityDocsURL(); got != "" {
		t.Fatalf("nil entry ObservabilityDocsURL() = %q, want empty", got)
	}
}

func TestGitOpsAndStackResolution(t *testing.T) {
	empty := &ClusterEntry{}
	if got := empty.ResolvedArgoCDNamespace(); got != "cicd" {
		t.Fatalf("ResolvedArgoCDNamespace() default = %q", got)
	}
	if got := empty.ResolvedApplicationsNamespace(); got != "cicd" {
		t.Fatalf("ResolvedApplicationsNamespace() default = %q", got)
	}
	if got := empty.ResolvedArgoCDServerMatch(); got != "argocd-server" {
		t.Fatalf("ResolvedArgoCDServerMatch() default = %q", got)
	}
	if got := empty.ResolvedStackNamespace(); got != "cicd" {
		t.Fatalf("ResolvedStackNamespace() default = %q", got)
	}
	if addons := empty.ResolvedStackAddons(); len(addons) != 3 {
		t.Fatalf("ResolvedStackAddons() default len = %d, want 3", len(addons))
	}

	withOverrides := &ClusterEntry{GitOps: GitOpsConfig{
		ArgoCDNamespace: "custom-argocd", ApplicationsNamespace: "custom-apps", ArgoCDServerMatch: "custom-server",
	}, Stack: StackConfig{Namespace: "custom-stack", Addons: []StackAddonSpec{{ID: "custom"}}}}
	if got := withOverrides.ResolvedArgoCDNamespace(); got != "custom-argocd" {
		t.Fatalf("ResolvedArgoCDNamespace() = %q", got)
	}
	if got := withOverrides.ResolvedApplicationsNamespace(); got != "custom-apps" {
		t.Fatalf("ResolvedApplicationsNamespace() = %q", got)
	}
	if got := withOverrides.ResolvedArgoCDServerMatch(); got != "custom-server" {
		t.Fatalf("ResolvedArgoCDServerMatch() = %q", got)
	}
	if got := withOverrides.ResolvedStackNamespace(); got != "custom-stack" {
		t.Fatalf("ResolvedStackNamespace() = %q", got)
	}
	if addons := withOverrides.ResolvedStackAddons(); len(addons) != 1 || addons[0].ID != "custom" {
		t.Fatalf("ResolvedStackAddons() = %+v", addons)
	}

	t.Setenv("PLATFORM_ARGOCD_NAMESPACE", "env-argocd")
	if got := withOverrides.ResolvedArgoCDNamespace(); got != "env-argocd" {
		t.Fatalf("ResolvedArgoCDNamespace() env override = %q", got)
	}
	t.Setenv("PLATFORM_GITOPS_APPS_NAMESPACE", "env-apps")
	if got := withOverrides.ResolvedApplicationsNamespace(); got != "env-apps" {
		t.Fatalf("ResolvedApplicationsNamespace() env override = %q", got)
	}
	t.Setenv("PLATFORM_STACK_NAMESPACE", "env-stack")
	if got := withOverrides.ResolvedStackNamespace(); got != "env-stack" {
		t.Fatalf("ResolvedStackNamespace() env override = %q", got)
	}
}

func TestResolvedGatewayURLsAndHosts(t *testing.T) {
	entry := &ClusterEntry{NodeIP: "10.0.0.1"}

	if got := entry.ResolvedStgGatewayURL(); got != "http://10.0.0.1:30880" {
		t.Fatalf("ResolvedStgGatewayURL() default = %q", got)
	}
	if got := entry.ResolvedDevGatewayURL(); got != "http://10.0.0.1:30882" {
		t.Fatalf("ResolvedDevGatewayURL() default = %q", got)
	}
	if got := entry.ResolvedProdGatewayURL(); got != "http://192.168.10.100" {
		t.Fatalf("ResolvedProdGatewayURL() default = %q", got)
	}
	if got := entry.ResolvedDevGatewayHost(); got != "dev.trader.bifrost.lan" {
		t.Fatalf("ResolvedDevGatewayHost() default = %q", got)
	}

	withSmoke := &ClusterEntry{
		StgSmoke:  StgSmokeConfig{GatewayURL: "http://stg.example", GatewayHost: "stg.example.lan"},
		ProdSmoke: StgSmokeConfig{GatewayURL: "http://prod.example", GatewayHost: "prod.example.lan", FrontendURL: "http://prod.example/app/", APIMonitorURL: "http://prod.example/monitor"},
		DevSmoke:  StgSmokeConfig{GatewayURL: "http://dev.example", GatewayHost: "dev.example.lan"},
	}
	if got := withSmoke.ResolvedStgGatewayURL(); got != "http://stg.example" {
		t.Fatalf("ResolvedStgGatewayURL() override = %q", got)
	}
	if got := withSmoke.ResolvedStgGatewayHost(); got != "stg.example.lan" {
		t.Fatalf("ResolvedStgGatewayHost() override = %q", got)
	}
	if got := withSmoke.ResolvedProdFrontendURL(); got != "http://prod.example/app/" {
		t.Fatalf("ResolvedProdFrontendURL() override = %q", got)
	}
	if got := withSmoke.ResolvedProdAPIMonitorURL(); got != "http://prod.example/monitor" {
		t.Fatalf("ResolvedProdAPIMonitorURL() override = %q", got)
	}

	// Fallback derivation when smoke config omits explicit frontend/monitor URLs.
	prodDefaults := &ClusterEntry{}
	if got := prodDefaults.ResolvedProdFrontendURL(); got != "http://192.168.10.100/" {
		t.Fatalf("ResolvedProdFrontendURL() fallback = %q", got)
	}
	if got := prodDefaults.ResolvedProdAPIMonitorURL(); got != "http://192.168.10.100/api/monitor/status" {
		t.Fatalf("ResolvedProdAPIMonitorURL() fallback = %q", got)
	}
	stgDefaults := &ClusterEntry{NodeIP: "10.0.0.1"}
	if got := stgDefaults.ResolvedStgFrontendURL(); got != "http://10.0.0.1:30880/" {
		t.Fatalf("ResolvedStgFrontendURL() fallback = %q", got)
	}
	if got := stgDefaults.ResolvedStgAPIMonitorURL(); got != "http://10.0.0.1:30880/api/monitor/status" {
		t.Fatalf("ResolvedStgAPIMonitorURL() fallback = %q", got)
	}

	t.Setenv("PLATFORM_STG_GATEWAY_URL", "http://env-stg")
	if got := withSmoke.ResolvedStgGatewayURL(); got != "http://env-stg" {
		t.Fatalf("ResolvedStgGatewayURL() env override = %q", got)
	}
	t.Setenv("PLATFORM_PROD_GATEWAY_URL", "http://env-prod")
	if got := withSmoke.ResolvedProdGatewayURL(); got != "http://env-prod" {
		t.Fatalf("ResolvedProdGatewayURL() env override = %q", got)
	}
	t.Setenv("PLATFORM_DEV_GATEWAY_URL", "http://env-dev")
	if got := withSmoke.ResolvedDevGatewayURL(); got != "http://env-dev" {
		t.Fatalf("ResolvedDevGatewayURL() env override = %q", got)
	}
}

func TestApplyGatewayHostHelpers(t *testing.T) {
	entry := &ClusterEntry{
		StgSmoke:  StgSmokeConfig{GatewayURL: "http://192.168.10.100", GatewayHost: "stg.bifrost.lan"},
		ProdSmoke: StgSmokeConfig{GatewayURL: "http://192.168.10.100", GatewayHost: "prod.bifrost.lan"},
		DevSmoke:  StgSmokeConfig{GatewayURL: "http://192.168.10.100", GatewayHost: "dev.bifrost.lan"},
	}

	req := httptest.NewRequest(http.MethodGet, "http://192.168.10.100/x", nil)
	entry.ApplyStgGatewayHost(req)
	if req.Host != "stg.bifrost.lan" {
		t.Fatalf("ApplyStgGatewayHost: Host = %q", req.Host)
	}

	req2 := httptest.NewRequest(http.MethodGet, "http://192.168.10.100/x", nil)
	entry.ApplyProdGatewayHost(req2)
	if req2.Host != "prod.bifrost.lan" {
		t.Fatalf("ApplyProdGatewayHost: Host = %q", req2.Host)
	}

	req3 := httptest.NewRequest(http.MethodGet, "http://192.168.10.100/x", nil)
	entry.ApplyDevGatewayHost(req3)
	if req3.Host != "dev.bifrost.lan" {
		t.Fatalf("ApplyDevGatewayHost: Host = %q", req3.Host)
	}

	// nil-safety.
	(*ClusterEntry)(nil).ApplyStgGatewayHost(req)
	(*ClusterEntry)(nil).ApplyProdGatewayHost(req)
	(*ClusterEntry)(nil).ApplyDevGatewayHost(req)
	entry.ApplyDevGatewayHost(nil)
}

func TestResolvedStgAPIDomains(t *testing.T) {
	if got := DefaultStgAPIDomains(); len(got) != 9 {
		t.Fatalf("DefaultStgAPIDomains() len = %d, want 9", len(got))
	}

	empty := &ClusterEntry{}
	if got := empty.ResolvedStgAPIDomains(); len(got) != 9 {
		t.Fatalf("ResolvedStgAPIDomains() default len = %d, want 9", len(got))
	}

	custom := &ClusterEntry{StgSmoke: StgSmokeConfig{APIDomains: []string{"monitor", " ", "trading"}}}
	if got := custom.ResolvedStgAPIDomains(); len(got) != 2 || got[0] != "monitor" || got[1] != "trading" {
		t.Fatalf("ResolvedStgAPIDomains() custom = %+v", got)
	}
}

func TestComputeNode(t *testing.T) {
	entry := &ClusterEntry{ComputeNodes: []ComputeNodeSpec{
		{Name: "gpu-server", SSHHost: "vision@10.0.0.60"},
	}}
	if got := entry.ComputeNode("gpu-server"); got == nil || got.SSHHost != "vision@10.0.0.60" {
		t.Fatalf("ComputeNode(gpu-server) = %+v", got)
	}
	if got := entry.ComputeNode("missing"); got != nil {
		t.Fatalf("ComputeNode(missing) = %+v, want nil", got)
	}
	if got := (*ClusterEntry)(nil).ComputeNode("gpu-server"); got != nil {
		t.Fatalf("ComputeNode on nil entry = %+v, want nil", got)
	}
	if got := entry.ComputeNode(""); got != nil {
		t.Fatalf("ComputeNode(empty name) = %+v, want nil", got)
	}
}

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	if got := expandHome("~/kube/config"); got != filepath.Join(home, "kube/config") {
		t.Fatalf("expandHome(~/x) = %q", got)
	}
	if got := expandHome("~"); got != home {
		t.Fatalf("expandHome(~) = %q, want %q", got, home)
	}
	if got := expandHome("/absolute/path"); got != "/absolute/path" {
		t.Fatalf("expandHome(absolute) = %q", got)
	}
}
