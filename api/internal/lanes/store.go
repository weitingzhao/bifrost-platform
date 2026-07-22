package lanes

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

type Store struct {
	path string
	mu   sync.Mutex
}

func NewStore(configDir string) *Store {
	return &Store{path: filepath.Join(configDir, "lanes.yaml")}
}

func (s *Store) Path() string { return s.path }

func (s *Store) Load() (*File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *Store) loadLocked() (*File, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("read lanes: %w", err)
	}
	var file File
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse lanes: %w", err)
	}
	if file.Version == "" {
		file.Version = "1"
	}
	if file.Lanes == nil {
		file.Lanes = []Lane{}
	}
	for i := range file.Lanes {
		if err := ValidateLane(file.Lanes[i]); err != nil {
			return nil, fmt.Errorf("lane %q: %w", file.Lanes[i].ID, err)
		}
	}
	return &file, nil
}

func (s *Store) saveLocked(file *File) error {
	if file.Version == "" {
		file.Version = "1"
	}
	data, err := yaml.Marshal(file)
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir lanes: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write lanes: %w", err)
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) List() (ListResponse, error) {
	file, err := s.Load()
	if err != nil {
		return ListResponse{}, err
	}
	return ListResponse{Version: file.Version, Lanes: file.Lanes}, nil
}

func (s *Store) Get(id string) (Lane, bool, error) {
	file, err := s.Load()
	if err != nil {
		return Lane{}, false, err
	}
	id = strings.TrimSpace(id)
	for _, l := range file.Lanes {
		if l.ID == id {
			return l, true, nil
		}
	}
	return Lane{}, false, nil
}

func (s *Store) Create(lane Lane) (Lane, error) {
	if err := ValidateLane(lane); err != nil {
		return Lane{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	file, err := s.loadLocked()
	if err != nil {
		return Lane{}, err
	}
	for _, existing := range file.Lanes {
		if existing.ID == lane.ID {
			return Lane{}, errf("lane already exists: " + lane.ID)
		}
	}
	file.Lanes = append(file.Lanes, lane)
	if err := s.saveLocked(file); err != nil {
		return Lane{}, err
	}
	return lane, nil
}

// Update merges non-empty patch fields into an existing lane and rewrites lanes.yaml.
// ID and Label are never changed.
func (s *Store) Update(id string, patch UpdateRequest) (Lane, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Lane{}, errf("id required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	file, err := s.loadLocked()
	if err != nil {
		return Lane{}, err
	}

	idx := -1
	for i, existing := range file.Lanes {
		if existing.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Lane{}, errf("lane not found: " + id)
	}

	updated := file.Lanes[idx]
	if v := strings.TrimSpace(patch.Track); v != "" {
		updated.Track = v
	}
	if v := strings.TrimSpace(patch.ComponentLine); v != "" {
		updated.ComponentLine = v
	}
	if v := strings.TrimSpace(patch.TrackType); v != "" {
		updated.TrackType = v
	}
	if v := strings.TrimSpace(patch.ShortLabel); v != "" {
		updated.ShortLabel = v
	}
	if v := strings.TrimSpace(patch.Description); v != "" {
		updated.Description = v
	}
	if v := strings.TrimSpace(patch.AgentMode); v != "" {
		updated.AgentMode = v
	}
	if v := strings.TrimSpace(patch.WorkIntent); v != "" {
		updated.WorkIntent = v
	}

	if err := ValidateLane(updated); err != nil {
		return Lane{}, err
	}

	file.Lanes[idx] = updated
	if err := s.saveLocked(file); err != nil {
		return Lane{}, err
	}
	return updated, nil
}

// Delete removes a lane by id from lanes.yaml.
func (s *Store) Delete(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errf("id required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	file, err := s.loadLocked()
	if err != nil {
		return err
	}

	idx := -1
	for i, existing := range file.Lanes {
		if existing.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return errf("lane not found: " + id)
	}

	file.Lanes = append(file.Lanes[:idx], file.Lanes[idx+1:]...)
	return s.saveLocked(file)
}
