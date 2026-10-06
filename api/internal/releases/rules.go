// Package releases records, per finished delivery PipelineRun, the exact commit
// each repo was built from, and keeps it past the CI system's own retention.
//
// Which runs count, and what environment each one ships to, is configuration
// (release-rules.yaml), not code: the platform knows pipelines and repos, not
// what the application in them does.
package releases

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const rulesFile = "release-rules.yaml"

// Rule maps finished PipelineRuns to a lane and an environment.
type Rule struct {
	Match struct {
		// Pipeline is the tekton.dev/pipeline label (a pipelineRef name).
		Pipeline string `yaml:"pipeline"`
		// NamePrefix matches the run name — for runs with an inline pipelineSpec,
		// whose tekton.dev/pipeline label is the run name itself.
		NamePrefix string `yaml:"name_prefix"`
	} `yaml:"match"`
	Lane string `yaml:"lane"`
	Env  string `yaml:"env"`
	// Deploys: the run puts what it built into a running environment. False for
	// image builds that something else (a pin, a GitOps sync) deploys later.
	Deploys bool `yaml:"deploys"`
}

// Rules is release-rules.yaml.
type Rules struct {
	// Namespace holds the PipelineRuns and the release records.
	Namespace string `yaml:"namespace"`
	// Rules are tried in order; the first match wins.
	Rules []Rule `yaml:"rules"`
}

// LoadRules reads PLATFORM_RELEASE_RULES or <configDir>/release-rules.yaml.
// A missing file is not an error: nothing is recorded.
func LoadRules(configDir string) (Rules, string, error) {
	path := strings.TrimSpace(os.Getenv("PLATFORM_RELEASE_RULES"))
	if path == "" {
		path = filepath.Join(configDir, rulesFile)
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Rules{}, path, nil
	}
	if err != nil {
		return Rules{}, path, err
	}
	var r Rules
	if err := yaml.Unmarshal(b, &r); err != nil {
		return Rules{}, path, fmt.Errorf("%s: %w", path, err)
	}
	for i, rule := range r.Rules {
		if rule.Match.Pipeline == "" && rule.Match.NamePrefix == "" {
			return Rules{}, path, fmt.Errorf("%s: rule %d matches nothing", path, i)
		}
		if rule.Lane == "" || rule.Env == "" {
			return Rules{}, path, fmt.Errorf("%s: rule %d needs lane and env", path, i)
		}
	}
	return r, path, nil
}

// Match returns the first rule for a run, by run name and tekton.dev/pipeline label.
func (r Rules) Match(runName, pipeline string) (Rule, bool) {
	for _, rule := range r.Rules {
		if rule.Match.NamePrefix != "" && strings.HasPrefix(runName, rule.Match.NamePrefix) {
			return rule, true
		}
		if rule.Match.Pipeline != "" && rule.Match.Pipeline == pipeline {
			return rule, true
		}
	}
	return Rule{}, false
}
