package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type ClusterEntry struct {
	ID                 string   `yaml:"id" json:"id"`
	Label              string   `yaml:"label" json:"label"`
	Distribution       string   `yaml:"distribution" json:"distribution"`
	APIServer          string   `yaml:"api_server" json:"api_server"`
	KubeconfigEnv      string   `yaml:"kubeconfig_env" json:"kubeconfig_env"`
	SSHHost            string   `yaml:"ssh_host" json:"ssh_host"`
	NodeIP             string   `yaml:"node_ip" json:"node_ip"`
	BifrostNamespaces  []string `yaml:"bifrost_namespaces" json:"bifrost_namespaces"`
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

func expandHome(path string) string {
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
