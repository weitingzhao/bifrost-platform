package migratewave

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

type waveYAML struct {
	ID         string `yaml:"id" json:"id"`
	Code       string `yaml:"code" json:"code"`
	SpineIndex int    `yaml:"spine_index" json:"spine_index"`
	Label      string `yaml:"label" json:"label"`
	Repo       string `yaml:"repo,omitempty" json:"repo,omitempty"`
	Verify     string `yaml:"verify,omitempty" json:"verify,omitempty"`
	BlockedBy  string `yaml:"blocked_by,omitempty" json:"blocked_by,omitempty"`
	Delivered  string `yaml:"delivered,omitempty" json:"delivered,omitempty"`
	Goal       string `yaml:"goal,omitempty" json:"goal,omitempty"`
}

type streamFile struct {
	StreamID string     `yaml:"stream_id" json:"stream_id"`
	Version  string     `yaml:"version" json:"version"`
	Waves    []waveYAML `yaml:"waves" json:"waves"`
}

var (
	catalogMu   sync.RWMutex
	catalogPath string
	loaded      bool
)

// InitMigrateWaveCatalog loads config/migrate-waves/*.yaml (called from NewHandler).
func InitMigrateWaveCatalog(configDir string) error {
	dir := filepath.Join(configDir, "migrate-waves")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read migrate-waves: %w", err)
	}
	m := make(map[string][]Wave)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var file streamFile
		if err := yaml.Unmarshal(data, &file); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		if file.StreamID == "" {
			return fmt.Errorf("%s: stream_id required", path)
		}
		waves := make([]Wave, 0, len(file.Waves))
		for _, w := range file.Waves {
			waves = append(waves, Wave(w))
		}
		m[file.StreamID] = waves
	}
	if len(m) == 0 {
		return fmt.Errorf("no migrate-wave catalogs in %s", dir)
	}
	catalogMu.Lock()
	streamWaves = m
	catalogPath = dir
	loaded = true
	catalogMu.Unlock()
	return nil
}

func ensureMigrateCatalog(configDir string) error {
	catalogMu.RLock()
	ok := loaded
	catalogMu.RUnlock()
	if ok {
		return nil
	}
	if configDir != "" {
		return InitMigrateWaveCatalog(configDir)
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
		if err := InitMigrateWaveCatalog(c); err == nil {
			return nil
		} else {
			last = err
		}
	}
	return last
}

// ListStreams returns loaded stream catalogs for API exposure.
func ListStreams() []streamFile {
	catalogMu.RLock()
	defer catalogMu.RUnlock()
	out := make([]streamFile, 0, len(streamWaves))
	for id, waves := range streamWaves {
		sf := streamFile{StreamID: id, Waves: make([]waveYAML, 0, len(waves))}
		for _, w := range waves {
			sf.Waves = append(sf.Waves, waveYAML(w))
		}
		out = append(out, sf)
	}
	return out
}
