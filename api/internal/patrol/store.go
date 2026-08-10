package patrol

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Store persists enable overlays + a 200-run ring buffer as JSON.
type Store struct {
	mu      sync.Mutex
	path    string
	enabled map[string]bool
	runs    []PatrolRun
}

func DefaultStateDir() string {
	if env := strings.TrimSpace(os.Getenv("PATROL_STATE_DIR")); env != "" {
		return env
	}
	if data := strings.TrimSpace(os.Getenv("PLATFORM_DATA_DIR")); data != "" {
		return filepath.Join(data, "patrol")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "bifrost-patrol")
	}
	return filepath.Join(home, ".bifrost-dev", "patrol")
}

func NewStore(dir string) (*Store, error) {
	if dir == "" {
		dir = DefaultStateDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir patrol state: %w", err)
	}
	s := &Store{
		path:    filepath.Join(dir, "state.json"),
		enabled: map[string]bool{},
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read patrol state: %w", err)
	}
	var rec persistState
	if err := json.Unmarshal(data, &rec); err != nil {
		return fmt.Errorf("parse patrol state: %w", err)
	}
	if rec.Enabled != nil {
		s.enabled = rec.Enabled
	}
	if rec.Runs != nil {
		s.runs = rec.Runs
		if len(s.runs) > MaxRuns {
			s.runs = s.runs[:MaxRuns]
		}
	}
	return nil
}

func (s *Store) saveLocked() error {
	rec := persistState{
		Enabled:   s.enabled,
		Runs:      s.runs,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write patrol state: %w", err)
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) Enabled(id string, yamlDefault bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.enabled[id]; ok {
		return v
	}
	return yamlDefault
}

func (s *Store) SetEnabled(id string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.enabled == nil {
		s.enabled = map[string]bool{}
	}
	s.enabled[id] = enabled
	return s.saveLocked()
}

func (s *Store) AppendRun(run PatrolRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs = append([]PatrolRun{run}, s.runs...)
	if len(s.runs) > MaxRuns {
		s.runs = s.runs[:MaxRuns]
	}
	return s.saveLocked()
}

func (s *Store) UpdateRun(id string, mutate func(*PatrolRun)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.runs {
		if s.runs[i].ID != id {
			continue
		}
		mutate(&s.runs[i])
		return s.saveLocked()
	}
	return fmt.Errorf("patrol run %s not found", id)
}

func (s *Store) ListRuns(limit int) ([]PatrolRun, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := len(s.runs)
	if limit <= 0 || limit > total {
		limit = total
	}
	out := make([]PatrolRun, limit)
	copy(out, s.runs[:limit])
	return out, total
}

func (s *Store) LastRun(skillID string) *PatrolRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.runs {
		if s.runs[i].SkillID == skillID {
			cp := s.runs[i]
			return &cp
		}
	}
	return nil
}
