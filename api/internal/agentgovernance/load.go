package agentgovernance

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

type taskYAML struct {
	ID             string   `yaml:"id"`
	Scope          string   `yaml:"scope"`
	Label          string   `yaml:"label"`
	Tier           string   `yaml:"tier"`
	DefaultLevel   string   `yaml:"default_level"`
	Domain         string   `yaml:"domain,omitempty"`
	Action         string   `yaml:"action,omitempty"`
	McpTools       []string `yaml:"mcp_tools"`
	MissionSignals []string `yaml:"mission_signals"`
}

type tasksFile struct {
	Version string     `yaml:"version"`
	Tasks   []taskYAML `yaml:"tasks"`
}

var (
	tasksMu     sync.RWMutex
	tasksCached []TaskDef
	tasksLoaded bool
)

func InitAgentTaskCatalog(configDir string) error {
	path := filepath.Join(configDir, "agent-tasks.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read agent-tasks: %w", err)
	}
	var file tasksFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("parse agent-tasks: %w", err)
	}
	if len(file.Tasks) == 0 {
		return fmt.Errorf("no tasks in %s", path)
	}
	out := make([]TaskDef, 0, len(file.Tasks))
	for _, t := range file.Tasks {
		out = append(out, TaskDef(t))
	}
	tasksMu.Lock()
	tasksCached = out
	tasksLoaded = true
	tasksMu.Unlock()
	return nil
}

func ensureAgentTasks() error {
	tasksMu.RLock()
	ok := tasksLoaded
	tasksMu.RUnlock()
	if ok {
		return nil
	}
	candidates := []string{
		filepath.Join("config"),
		filepath.Join("..", "config"),
		filepath.Join("..", "..", "..", "config"),
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "..", "..", "..", "config"))
	}
	var last error
	for _, c := range candidates {
		if err := InitAgentTaskCatalog(c); err == nil {
			return nil
		} else {
			last = err
		}
	}
	return last
}
