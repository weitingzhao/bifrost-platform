package devsession

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const sessionsCatalogFile = "sessions-catalog.yaml"

// Annotation keys for opt-in Deployment discovery (complements the static catalog).
const (
	AnnotationSession      = "bifrost.dev/session"
	AnnotationSessionName  = "bifrost.dev/session-name"
	AnnotationSessionLabel = "bifrost.dev/session-label"
	AnnotationSessionGroup = "bifrost.dev/session-group"
)

// CatalogEntry describes one allowlisted session for a viewer env.
type CatalogEntry struct {
	Name       string `yaml:"name" json:"name"`
	Label      string `yaml:"label" json:"label"`
	Group      string `yaml:"group" json:"group"`
	Namespace  string `yaml:"namespace" json:"namespace"`
	Deployment string `yaml:"deployment" json:"deployment"`
	Ports      []int  `yaml:"ports,omitempty" json:"ports,omitempty"`
}

// DiscoveryConfig controls annotation-based session discovery.
type DiscoveryConfig struct {
	Enabled    bool                `yaml:"enabled"`
	Namespaces map[string][]string `yaml:"namespaces"`
}

// SessionsCatalog is the static allowlist for K8s-mode sessions.
type SessionsCatalog struct {
	Version   string                    `yaml:"version"`
	Discovery DiscoveryConfig           `yaml:"discovery"`
	Envs      map[string][]CatalogEntry `yaml:"envs"`
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
	if cat.Discovery.Namespaces == nil {
		cat.Discovery.Namespaces = map[string][]string{}
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
	for env, nss := range cat.Discovery.Namespaces {
		out := make([]string, 0, len(nss))
		seen := map[string]struct{}{}
		for _, ns := range nss {
			ns = strings.TrimSpace(ns)
			if ns == "" {
				continue
			}
			if _, ok := seen[ns]; ok {
				continue
			}
			seen[ns] = struct{}{}
			out = append(out, ns)
		}
		cat.Discovery.Namespaces[strings.ToLower(strings.TrimSpace(env))] = out
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

// DiscoveryNamespacesForEnv returns namespaces to scan for annotated Deployments.
// When discovery is enabled but no explicit list is set, falls back to namespaces
// referenced by catalog entries for that env.
func (c *SessionsCatalog) DiscoveryNamespacesForEnv(env string) []string {
	if c == nil || !c.Discovery.Enabled {
		return nil
	}
	env = strings.ToLower(strings.TrimSpace(env))
	if nss, ok := c.Discovery.Namespaces[env]; ok && len(nss) > 0 {
		return nss
	}
	seen := map[string]struct{}{}
	var out []string
	for _, e := range c.EntriesForEnv(env) {
		if e.Namespace == "" {
			continue
		}
		if _, ok := seen[e.Namespace]; ok {
			continue
		}
		seen[e.Namespace] = struct{}{}
		out = append(out, e.Namespace)
	}
	return out
}

func sessionAnnotationEnabled(annotations map[string]string) bool {
	if annotations == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(annotations[AnnotationSession])) {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}

func entryFromAnnotatedDeployment(namespace string, deployName string, annotations map[string]string, containerPorts []int) CatalogEntry {
	name := strings.TrimSpace(annotations[AnnotationSessionName])
	if name == "" {
		name = deployName
	}
	label := strings.TrimSpace(annotations[AnnotationSessionLabel])
	if label == "" {
		label = name
	}
	group := strings.TrimSpace(annotations[AnnotationSessionGroup])
	if group == "" {
		group = "discovered"
	}
	return CatalogEntry{
		Name:       name,
		Label:      label,
		Group:      group,
		Namespace:  namespace,
		Deployment: deployName,
		Ports:      containerPorts,
	}
}
