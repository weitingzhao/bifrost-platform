package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type ObservabilityURLs struct {
	Grafana    string `yaml:"grafana"`
	Prometheus string `yaml:"prometheus"`
	DocsInfra  string `yaml:"docs_infra"`
}

// GitOpsConfig — Argo CD probe targets (P3 L0 read).
type GitOpsConfig struct {
	ArgoCDNamespace       string `yaml:"argocd_namespace" json:"argocd_namespace"`
	ApplicationsNamespace string `yaml:"applications_namespace" json:"applications_namespace"`
	ArgoCDServerMatch     string `yaml:"argocd_server_match" json:"argocd_server_match"`
}

// StackAddonSpec — CI/CD stack component probe (P2 L0 read).
type StackAddonSpec struct {
	ID              string `yaml:"id" json:"id"`
	Label           string `yaml:"label" json:"label"`
	Match           string `yaml:"match" json:"match"`
	ProbeNamespace  string `yaml:"probe_namespace" json:"probe_namespace,omitempty"`
}

// StackConfig — delivery stack probes in cicd namespace.
type StackConfig struct {
	Namespace string           `yaml:"namespace" json:"namespace"`
	Addons    []StackAddonSpec `yaml:"addons" json:"addons"`
}

// StgSmokeConfig — HTTP probes for bifrost-stg workloads (Session S5 / Phase B v2).
type StgSmokeConfig struct {
	GatewayURL    string   `yaml:"gateway_url" json:"gateway_url"`
	APIMonitorURL string   `yaml:"api_monitor_url" json:"api_monitor_url"`
	FrontendURL   string   `yaml:"frontend_url" json:"frontend_url"`
	APIDomains    []string `yaml:"api_domains" json:"api_domains"`
}

// DefaultStgAPIDomains — FastAPI domains behind nginx /api/{domain}/.
func DefaultStgAPIDomains() []string {
	return []string{
		"monitor", "massive", "docs", "ops", "trading",
		"strategy", "portfolio", "market", "research",
	}
}

type ClusterEntry struct {
	ID                  string             `yaml:"id" json:"id"`
	Label               string             `yaml:"label" json:"label"`
	Distribution        string             `yaml:"distribution" json:"distribution"`
	APIServer           string             `yaml:"api_server" json:"api_server"`
	KubeconfigEnv       string             `yaml:"kubeconfig_env" json:"kubeconfig_env"`
	SSHHost             string             `yaml:"ssh_host" json:"ssh_host"`
	NodeIP              string             `yaml:"node_ip" json:"node_ip"`
	BifrostNamespaces   []string           `yaml:"bifrost_namespaces" json:"bifrost_namespaces"`
	MonitoringNS        string             `yaml:"monitoring_namespace" json:"monitoring_namespace"`
	ObservabilityURLs   ObservabilityURLs  `yaml:"observability_urls" json:"observability_urls"`
	GitOps              GitOpsConfig       `yaml:"gitops" json:"gitops"`
	Stack               StackConfig        `yaml:"stack" json:"stack"`
	StgSmoke            StgSmokeConfig     `yaml:"stg_smoke" json:"stg_smoke"`
}

type ClustersFile struct {
	Clusters []ClusterEntry `yaml:"clusters"`
}

func LoadClusters(configDir string) (*ClustersFile, string, error) {
	path := os.Getenv("PLATFORM_CLUSTERS")
	if path == "" {
		if configDir != "" {
			path = filepath.Join(configDir, "clusters.yaml")
		} else {
			path = defaultClustersPath()
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read clusters %s: %w", path, err)
	}

	var file ClustersFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, "", fmt.Errorf("parse clusters: %w", err)
	}
	if len(file.Clusters) == 0 {
		return nil, "", fmt.Errorf("no clusters in %s", path)
	}
	return &file, path, nil
}

func defaultClustersPath() string {
	if wd, err := os.Getwd(); err == nil {
		for _, p := range []string{
			filepath.Join(wd, "config", "clusters.yaml"),
			filepath.Join(wd, "..", "config", "clusters.yaml"),
		} {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return "config/clusters.yaml"
}

func (c *Config) DefaultCluster() *ClusterEntry {
	if c.Clusters == nil || len(c.Clusters.Clusters) == 0 {
		return nil
	}
	return &c.Clusters.Clusters[0]
}

func (e *ClusterEntry) KubeconfigPath() string {
	envName := e.KubeconfigEnv
	if envName == "" {
		envName = "PLATFORM_KUBECONFIG"
	}
	if v := os.Getenv(envName); v != "" {
		return expandHome(v)
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		return filepath.Join(home, ".kube", "bifrost-k3s.yaml")
	}
	return ""
}

func (e *ClusterEntry) ResolvedMonitoringNamespace() string {
	if e == nil || e.MonitoringNS == "" {
		return "monitoring"
	}
	return e.MonitoringNS
}

func (e *ClusterEntry) GrafanaURL() string {
	if v := strings.TrimSpace(os.Getenv("PLATFORM_GRAFANA_URL")); v != "" {
		return v
	}
	if e != nil {
		return strings.TrimSpace(e.ObservabilityURLs.Grafana)
	}
	return ""
}

func (e *ClusterEntry) PrometheusURL() string {
	if v := strings.TrimSpace(os.Getenv("PLATFORM_PROMETHEUS_URL")); v != "" {
		return v
	}
	if e != nil {
		return strings.TrimSpace(e.ObservabilityURLs.Prometheus)
	}
	return ""
}

func (e *ClusterEntry) ObservabilityDocsURL() string {
	if e != nil && strings.TrimSpace(e.ObservabilityURLs.DocsInfra) != "" {
		return strings.TrimSpace(e.ObservabilityURLs.DocsInfra)
	}
	return ""
}

func (e *ClusterEntry) ResolvedArgoCDNamespace() string {
	if v := strings.TrimSpace(os.Getenv("PLATFORM_ARGOCD_NAMESPACE")); v != "" {
		return v
	}
	if e != nil && strings.TrimSpace(e.GitOps.ArgoCDNamespace) != "" {
		return strings.TrimSpace(e.GitOps.ArgoCDNamespace)
	}
	return "cicd"
}

func (e *ClusterEntry) ResolvedApplicationsNamespace() string {
	if v := strings.TrimSpace(os.Getenv("PLATFORM_GITOPS_APPS_NAMESPACE")); v != "" {
		return v
	}
	if e != nil && strings.TrimSpace(e.GitOps.ApplicationsNamespace) != "" {
		return strings.TrimSpace(e.GitOps.ApplicationsNamespace)
	}
	if e != nil {
		return e.ResolvedArgoCDNamespace()
	}
	return "cicd"
}

func (e *ClusterEntry) ResolvedArgoCDServerMatch() string {
	if e != nil && strings.TrimSpace(e.GitOps.ArgoCDServerMatch) != "" {
		return strings.TrimSpace(e.GitOps.ArgoCDServerMatch)
	}
	return "argocd-server"
}

func (e *ClusterEntry) ResolvedStackNamespace() string {
	if v := strings.TrimSpace(os.Getenv("PLATFORM_STACK_NAMESPACE")); v != "" {
		return v
	}
	if e != nil && strings.TrimSpace(e.Stack.Namespace) != "" {
		return strings.TrimSpace(e.Stack.Namespace)
	}
	return "cicd"
}

func (e *ClusterEntry) ResolvedStackAddons() []StackAddonSpec {
	if e != nil && len(e.Stack.Addons) > 0 {
		return e.Stack.Addons
	}
	return []StackAddonSpec{
		{ID: "gitea", Label: "Gitea", Match: "gitea"},
		{ID: "tekton", Label: "Tekton", Match: "tekton"},
		{ID: "registry", Label: "Registry", Match: "registry"},
	}
}

func (e *ClusterEntry) ResolvedStgGatewayURL() string {
	if v := strings.TrimSpace(os.Getenv("PLATFORM_STG_GATEWAY_URL")); v != "" {
		return v
	}
	if e != nil && strings.TrimSpace(e.StgSmoke.GatewayURL) != "" {
		return strings.TrimSpace(e.StgSmoke.GatewayURL)
	}
	if e != nil && strings.TrimSpace(e.NodeIP) != "" {
		return fmt.Sprintf("http://%s:30880", strings.TrimSpace(e.NodeIP))
	}
	return ""
}

func (e *ClusterEntry) ResolvedStgAPIDomains() []string {
	if e != nil && len(e.StgSmoke.APIDomains) > 0 {
		out := make([]string, 0, len(e.StgSmoke.APIDomains))
		for _, d := range e.StgSmoke.APIDomains {
			if s := strings.TrimSpace(d); s != "" {
				out = append(out, s)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return DefaultStgAPIDomains()
}

func (e *ClusterEntry) ResolvedStgAPIMonitorURL() string {
	if v := strings.TrimSpace(os.Getenv("PLATFORM_STG_API_MONITOR_URL")); v != "" {
		return v
	}
	if e != nil && strings.TrimSpace(e.StgSmoke.APIMonitorURL) != "" {
		return strings.TrimSpace(e.StgSmoke.APIMonitorURL)
	}
	if gw := e.ResolvedStgGatewayURL(); gw != "" {
		return strings.TrimRight(gw, "/") + "/api/monitor/status"
	}
	return ""
}

func (e *ClusterEntry) ResolvedStgFrontendURL() string {
	if v := strings.TrimSpace(os.Getenv("PLATFORM_STG_FRONTEND_URL")); v != "" {
		return v
	}
	if e != nil && strings.TrimSpace(e.StgSmoke.FrontendURL) != "" {
		return strings.TrimSpace(e.StgSmoke.FrontendURL)
	}
	if gw := e.ResolvedStgGatewayURL(); gw != "" {
		return strings.TrimRight(gw, "/") + "/"
	}
	return ""
}

func expandHome(path string) string {
	path = os.ExpandEnv(path)
	if len(path) >= 2 && path[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	if path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
	}
	return path
}
