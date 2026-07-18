package agentgovernance

// TaskDef mirrors console agentTaskCatalog scopes for Flight Director trust matrix.
type TaskDef struct {
	ID             string   `json:"id"`
	Scope          string   `json:"scope"`
	Label          string   `json:"label"`
	Tier           string   `json:"tier"` // manual | automated | escalation
	DefaultLevel   string   `json:"default_level"`
	Domain         string   `json:"domain,omitempty"`
	Action         string   `json:"action,omitempty"`
	McpTools       []string `json:"mcp_tools,omitempty"`
	MissionSignals []string `json:"mission_signals,omitempty"`
}

// TaskCatalog returns the YAML-backed agent task list (config/agent-tasks.yaml).
func TaskCatalog() []TaskDef {
	_ = ensureAgentTasks()
	tasksMu.RLock()
	defer tasksMu.RUnlock()
	out := make([]TaskDef, len(tasksCached))
	copy(out, tasksCached)
	return out
}

// TaskByID resolves a task from the config/agent-tasks.yaml SSOT.
func TaskByID(id string) (TaskDef, bool) {
	for _, task := range TaskCatalog() {
		if task.ID == id {
			return task, true
		}
	}
	return TaskDef{}, false
}

func scopeAliases() map[string]string {
	m := make(map[string]string)
	for _, t := range TaskCatalog() {
		m[t.Scope] = t.Scope
	}
	m["Nightly scheduled health verification"] = "nightly-health-check"
	m[""] = "agent-desk"
	return m
}

func normalizeScope(scope string) string {
	if s, ok := scopeAliases()[scope]; ok {
		return s
	}
	return scope
}

func taskByScope(scope string) (TaskDef, bool) {
	n := normalizeScope(scope)
	for _, t := range TaskCatalog() {
		if t.Scope == n {
			return t, true
		}
	}
	return TaskDef{}, false
}
