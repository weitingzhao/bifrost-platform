package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/weitingzhao/bifrost-platform/api/internal/opscontext"
	"gopkg.in/yaml.v3"
)

type HostPort struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type Environment struct {
	ID          string   `yaml:"id" json:"id"`
	Label       string   `yaml:"label" json:"label"`
	NginxBase   string   `yaml:"nginx_base" json:"nginx_base"`
	Postgres    HostPort `yaml:"postgres" json:"postgres"`
	Redis       HostPort `yaml:"redis" json:"redis"`
	OpsTokenEnv string   `yaml:"ops_token_env" json:"ops_token_env,omitempty"`
}

type File struct {
	Environments []Environment `yaml:"environments"`
}

type Config struct {
	Environments     []Environment
	Topology         *TopologyFile
	Clusters         *ClustersFile
	OpsContext       *opscontext.File
	Listen           string
	ConfigPath       string
	TopologyPath     string
	ClustersPath     string
	PlatformAuthPath string
	OpsContextPath   string
}

func Load() (*Config, error) {
	configPath := os.Getenv("PLATFORM_CONFIG")
	if configPath == "" {
		configPath = defaultConfigPath()
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", configPath, err)
	}

	var file File
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if len(file.Environments) == 0 {
		return nil, fmt.Errorf("no environments in %s", configPath)
	}

	listen := os.Getenv("PLATFORM_LISTEN")
	if listen == "" {
		listen = ":8780"
	}

	topo, topoPath, err := LoadTopology(TopologyDirFromConfigPath(configPath))
	if err != nil {
		return nil, err
	}

	configDir := TopologyDirFromConfigPath(configPath)
	clusters, clustersPath, err := LoadClusters(configDir)
	if err != nil {
		return nil, err
	}
	platformAuthPath := ResolvePlatformAuthPath(configDir)

	opsCtxPath := opscontext.ResolvePath(configPath)
	opsCtx, err := opscontext.Load(opsCtxPath)
	if err != nil {
		return nil, err
	}

	return &Config{
		Environments:     file.Environments,
		Topology:         topo,
		Clusters:         clusters,
		OpsContext:       opsCtx,
		Listen:           listen,
		ConfigPath:       configPath,
		TopologyPath:     topoPath,
		ClustersPath:     clustersPath,
		PlatformAuthPath: platformAuthPath,
		OpsContextPath:   opsCtxPath,
	}, nil
}

func defaultConfigPath() string {
	if wd, err := os.Getwd(); err == nil {
		candidates := []string{
			filepath.Join(wd, "config", "environments.yaml"),
			filepath.Join(wd, "..", "config", "environments.yaml"),
		}
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return "config/environments.yaml"
}

func (c *Config) ConfigDir() string {
	return TopologyDirFromConfigPath(c.ConfigPath)
}

func (c *Config) GetEnvironment(id string) (*Environment, bool) {
	for i := range c.Environments {
		if c.Environments[i].ID == id {
			return &c.Environments[i], true
		}
	}
	return nil, false
}

func (e *Environment) OpsToken() string {
	if e.OpsTokenEnv == "" {
		return ""
	}
	return os.Getenv(e.OpsTokenEnv)
}
