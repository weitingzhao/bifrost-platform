package agentgovernance

import (
	"context"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// TrustOverride is one Owner actuation level from config/trust-overrides.yaml.
type TrustOverride struct {
	SkillID   string `json:"skill_id" yaml:"skill_id"`
	Level     string `json:"level" yaml:"level"`
	Reason    string `json:"reason,omitempty" yaml:"reason,omitempty"`
	AppliedBy string `json:"applied_by,omitempty" yaml:"applied_by"`
	AppliedAt string `json:"applied_at,omitempty" yaml:"applied_at"`
}

// TrustOverrideStore is the read-only file of Owner trust levels.
// A missing or unreadable file is an error so the handler can say store_error
// and still return the matrix. It is not a reason to answer 500.
type TrustOverrideStore interface {
	List(ctx context.Context) (map[string]TrustOverride, error)
	// Location is the file path the response reports as store.
	Location() string
}

type trustOverrideFile struct {
	Overrides []TrustOverride `yaml:"overrides"`
}

// NewYAMLTrustOverrideStore reads path on every List. The file is the release
// artifact; there is no write API.
func NewYAMLTrustOverrideStore(path string) TrustOverrideStore {
	return yamlTrustOverrides{path: path}
}

type yamlTrustOverrides struct {
	path string
}

func (s yamlTrustOverrides) Location() string { return s.path }

func (s yamlTrustOverrides) List(context.Context) (map[string]TrustOverride, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("read trust overrides %s: %w", s.path, err)
	}
	var doc trustOverrideFile
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse trust overrides %s: %w", s.path, err)
	}
	out := make(map[string]TrustOverride, len(doc.Overrides))
	for _, o := range doc.Overrides {
		if o.SkillID == "" {
			continue
		}
		out[o.SkillID] = o
	}
	return out, nil
}
