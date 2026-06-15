package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type GridPos struct {
	Row int `yaml:"row" json:"row"`
	Col int `yaml:"col" json:"col"`
}

type TopologyNode struct {
	ID            string   `yaml:"id" json:"id"`
	Label         string   `yaml:"label" json:"label"`
	Host          string   `yaml:"host" json:"host,omitempty"`
	Group         string   `yaml:"group" json:"group"`
	ComposeRoles  []string `yaml:"compose_roles" json:"compose_roles"`
	K3sRoles      []string `yaml:"k3s_roles" json:"k3s_roles"`
	InK3sCluster  bool     `yaml:"in_k3s_cluster" json:"in_k3s_cluster"`
	Grid          GridPos  `yaml:"grid" json:"grid"`
	MatrixTargets []string `yaml:"matrix_targets" json:"matrix_targets,omitempty"`
	SSHPort       int      `yaml:"ssh_port,omitempty" json:"ssh_port,omitempty"`
	SSHUser       string   `yaml:"ssh_user,omitempty" json:"ssh_user,omitempty"`
	SSHJump       string   `yaml:"ssh_jump,omitempty" json:"ssh_jump,omitempty"` // topology node id — ProxyJump via bastion
}

type TopologyEdge struct {
	ID           string   `yaml:"id" json:"id"`
	From         string   `yaml:"from" json:"from"`
	To           string   `yaml:"to" json:"to"`
	Label        string   `yaml:"label" json:"label"`
	Kind         string   `yaml:"kind" json:"kind"`
	Environments []string `yaml:"environments" json:"environments"`
	MatrixTarget string   `yaml:"matrix_target" json:"matrix_target,omitempty"`
}

type TopologyFile struct {
	DeploymentPhase string         `yaml:"deployment_phase" json:"deployment_phase"`
	Nodes           []TopologyNode `yaml:"nodes" json:"nodes"`
	Edges           []TopologyEdge `yaml:"edges" json:"edges"`
}

func LoadTopology(configDir string) (*TopologyFile, string, error) {
	path := os.Getenv("PLATFORM_TOPOLOGY")
	if path == "" {
		if configDir != "" {
			path = filepath.Join(configDir, "topology.yaml")
		} else {
			path = defaultTopologyPath()
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read topology %s: %w", path, err)
	}

	var file TopologyFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, "", fmt.Errorf("parse topology: %w", err)
	}
	if len(file.Nodes) == 0 {
		return nil, "", fmt.Errorf("no nodes in %s", path)
	}
	if file.DeploymentPhase == "" {
		file.DeploymentPhase = "compose"
	}
	return &file, path, nil
}

func defaultTopologyPath() string {
	if wd, err := os.Getwd(); err == nil {
		for _, p := range []string{
			filepath.Join(wd, "config", "topology.yaml"),
			filepath.Join(wd, "..", "config", "topology.yaml"),
		} {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return "config/topology.yaml"
}

func TopologyDirFromConfigPath(configPath string) string {
	return filepath.Dir(configPath)
}
