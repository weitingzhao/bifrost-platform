package config

import (
	"fmt"
	"os"
	"path/filepath"

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
	Environments []Environment
	Listen       string
	ConfigPath   string
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

	return &Config{
		Environments: file.Environments,
		Listen:       listen,
		ConfigPath:   configPath,
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
