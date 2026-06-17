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
