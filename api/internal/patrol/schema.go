package patrol

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/mcp"
	"gopkg.in/yaml.v3"
)

var skillIDRe = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)

// DefaultSkillsDir returns config/patrol-skills under the platform config dir,
// overridable via PATROL_SKILLS_DIR.
func DefaultSkillsDir(configDir string) string {
	if env := strings.TrimSpace(os.Getenv("PATROL_SKILLS_DIR")); env != "" {
		return env
	}
	if strings.TrimSpace(configDir) == "" {
		return "config/patrol-skills"
	}
	return filepath.Join(configDir, "patrol-skills")
}

// LoadDir parses every non-underscore YAML in dir. Missing dir → empty list.
func LoadDir(dir string) ([]PatrolSkill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read patrol skills dir %s: %w", dir, err)
	}
	writeTools := writeToolSet()
	catalog := catalogToolSet()
	var out []PatrolSkill
	seen := map[string]string{}
	for _, ent := range entries {
		name := ent.Name()
		if ent.IsDir() || strings.HasPrefix(name, "_") {
			continue
		}
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		path := filepath.Join(dir, name)
		skill, err := loadSkillFile(path, catalog, writeTools)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if prev, ok := seen[skill.ID]; ok {
			return nil, fmt.Errorf("duplicate patrol skill id %q (%s and %s)", skill.ID, prev, path)
		}
		seen[skill.ID] = path
		out = append(out, skill)
	}
	return out, nil
}

func loadSkillFile(path string, catalog, writeTools map[string]struct{}) (PatrolSkill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return PatrolSkill{}, err
	}
	var doc SkillYAML
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return PatrolSkill{}, fmt.Errorf("parse: %w", err)
	}
	return validateSkill(doc, catalog, writeTools)
}

func validateSkill(doc SkillYAML, catalog, writeTools map[string]struct{}) (PatrolSkill, error) {
	id := strings.TrimSpace(doc.ID)
	if !skillIDRe.MatchString(id) {
		return PatrolSkill{}, fmt.Errorf("invalid id %q", doc.ID)
	}
	name := strings.TrimSpace(doc.Name)
	if name == "" {
		return PatrolSkill{}, fmt.Errorf("name required")
	}
	desc := strings.TrimSpace(doc.Description)
	if desc == "" {
		return PatrolSkill{}, fmt.Errorf("description required")
	}
	sched := strings.TrimSpace(doc.Schedule)
	if _, err := ParseCron(sched); err != nil {
		return PatrolSkill{}, fmt.Errorf("schedule: %w", err)
	}
	prompt := strings.TrimSpace(doc.PromptTemplate)
	if prompt == "" {
		return PatrolSkill{}, fmt.Errorf("prompt_template required")
	}
	trust := TrustLevel(strings.TrimSpace(doc.TrustLevel))
	switch trust {
	case TrustL0, TrustL1, TrustL2:
	default:
		return PatrolSkill{}, fmt.Errorf("trust_level must be L0, L1, or L2")
	}
	scope := strings.TrimSpace(doc.Scope)
	if scope == "" {
		return PatrolSkill{}, fmt.Errorf("scope required")
	}
	timeout := doc.Timeout
	if doc.TimeoutSeconds > 0 {
		timeout = doc.TimeoutSeconds
	}
	if timeout <= 0 {
		return PatrolSkill{}, fmt.Errorf("timeout must be > 0 seconds")
	}
	if timeout > 3600 {
		return PatrolSkill{}, fmt.Errorf("timeout %d exceeds 3600s", timeout)
	}
	tools := make([]string, 0, len(doc.MCPTools))
	for _, raw := range doc.MCPTools {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		if _, ok := catalog[t]; !ok {
			return PatrolSkill{}, fmt.Errorf("unknown mcp tool %q", t)
		}
		tools = append(tools, t)
	}
	if len(tools) == 0 {
		return PatrolSkill{}, fmt.Errorf("mcp_tools must list at least one catalog tool")
	}
	if trust == TrustL0 {
		if writes := writeToolsIn(tools, writeTools); len(writes) > 0 {
			return PatrolSkill{}, fmt.Errorf("L0 skill cannot include write tools: %s", strings.Join(writes, ", "))
		}
	}
	cronAct := strings.TrimSpace(doc.CronActuation)
	if cronAct == "" {
		cronAct = CronActuationEscalate
	}
	if cronAct != CronActuationEscalate && cronAct != CronActuationConfirm {
		return PatrolSkill{}, fmt.Errorf("cron_actuation must be escalate or confirm")
	}
	enabled := true
	if doc.Enabled != nil {
		enabled = *doc.Enabled
	}
	return PatrolSkill{
		ID:             id,
		Name:           name,
		Description:    desc,
		Schedule:       sched,
		PromptTemplate: prompt,
		MCPTools:       tools,
		TrustLevel:     trust,
		Scope:          scope,
		TimeoutSeconds: timeout,
		Enabled:        enabled,
		CronActuation:  cronAct,
	}, nil
}

func catalogToolSet() map[string]struct{} {
	out := map[string]struct{}{}
	for _, t := range mcp.Catalog() {
		out[t.Name] = struct{}{}
	}
	return out
}

func writeToolSet() map[string]struct{} {
	out := map[string]struct{}{}
	for _, t := range mcp.Catalog() {
		if !isReadTool(t.Level, t.Method) {
			out[t.Name] = struct{}{}
		}
	}
	return out
}

func isReadTool(level, method string) bool {
	if strings.EqualFold(level, "read") {
		return true
	}
	m := strings.ToUpper(strings.TrimSpace(method))
	return m == "GET" || m == "" && level == ""
}

func writeToolsIn(tools []string, writeSet map[string]struct{}) []string {
	var out []string
	for _, t := range tools {
		if _, ok := writeSet[t]; ok {
			out = append(out, t)
		}
	}
	return out
}
