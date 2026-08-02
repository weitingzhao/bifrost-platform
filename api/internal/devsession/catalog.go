package devsession

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const sessionsCatalogFile = "sessions-catalog.yaml"

// CatalogEntry describes one allowlisted session for a viewer env.
type CatalogEntry struct {
	Name       string `yaml:"name" json:"name"`
	Label      string `yaml:"label" json:"label"`
	Group      string `yaml:"group" json:"group"`
	Namespace  string `yaml:"namespace" json:"namespace"`
	Deployment string `yaml:"deployment" json:"deployment"`
	Ports      []int  `yaml:"ports,omitempty" json:"ports,omitempty"`
}

// SessionsCatalog is the static allowlist for K8s-mode sessions.
type SessionsCatalog struct {
	Version string                    `yaml:"version"`
	Envs    map[string][]CatalogEntry `yaml:"envs"`
}

// LoadSessionsCatalog reads config/sessions-catalog.yaml from configDir.
// Missing file returns an empty catalog (not an error) so local bdev mode is unaffected.
func LoadSessionsCatalog(configDir string) (*SessionsCatalog, error) {
	path := filepath.Join(configDir, sessionsCatalogFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &SessionsCatalog{Envs: map[string][]CatalogEntry{}}, nil
		}
		return nil, fmt.Errorf("read sessions catalog: %w", err)
	}
	var cat SessionsCatalog
	if err := yaml.Unmarshal(data, &cat); err != nil {
		return nil, fmt.Errorf("parse sessions catalog: %w", err)
	}
	if cat.Envs == nil {
		cat.Envs = map[string][]CatalogEntry{}
	}
	for env, entries := range cat.Envs {
		normalized := make([]CatalogEntry, 0, len(entries))
		for _, e := range entries {
			e.Name = strings.TrimSpace(e.Name)
			e.Label = strings.TrimSpace(e.Label)
			e.Group = strings.TrimSpace(e.Group)
			e.Namespace = strings.TrimSpace(e.Namespace)
			e.Deployment = strings.TrimSpace(e.Deployment)
			if e.Name == "" || e.Namespace == "" {
				continue
			}
			if e.Deployment == "" {
				e.Deployment = e.Name
			}
			if e.Label == "" {
				e.Label = e.Name
			}
			if e.Group == "" {
				e.Group = "other"
			}
			normalized = append(normalized, e)
		}
		cat.Envs[env] = normalized
	}
	return &cat, nil
}

// EntriesForEnv returns catalog rows for the viewer seat (stg/prod).
func (c *SessionsCatalog) EntriesForEnv(env string) []CatalogEntry {
	if c == nil {
		return nil
	}
	env = strings.ToLower(strings.TrimSpace(env))
	if entries, ok := c.Envs[env]; ok {
		return entries
	}
	return nil
}

// Lookup returns the catalog entry for name within env, or nil.
func (c *SessionsCatalog) Lookup(env, name string) *CatalogEntry {
	entries := c.EntriesForEnv(env)
	for i := range entries {
		if entries[i].Name == name {
			e := entries[i]
			return &e
		}
	}
	return nil
}
