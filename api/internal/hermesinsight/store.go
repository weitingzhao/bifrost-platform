package hermesinsight

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Store persists a 200-insight ring buffer as JSON (Patrol store pattern).
type Store struct {
	mu       sync.Mutex
	path     string
	insights []HermesInsight
}

func DefaultStateDir() string {
	if env := strings.TrimSpace(os.Getenv("HERMES_INSIGHT_STATE_DIR")); env != "" {
		return env
	}
	if data := strings.TrimSpace(os.Getenv("PLATFORM_DATA_DIR")); data != "" {
		return filepath.Join(data, "hermes-insights")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "bifrost-hermes-insights")
	}
	return filepath.Join(home, ".bifrost-dev", "hermes-insights")
}

func NewStore(dir string) (*Store, error) {
	if dir == "" {
		dir = DefaultStateDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir hermes-insight state: %w", err)
	}
	s := &Store{path: filepath.Join(dir, "state.json")}
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
		return fmt.Errorf("read hermes-insight state: %w", err)
	}
	var rec persistState
	if err := json.Unmarshal(data, &rec); err != nil {
		return fmt.Errorf("parse hermes-insight state: %w", err)
	}
	if rec.Insights != nil {
		s.insights = rec.Insights
		if len(s.insights) > MaxInsights {
			s.insights = s.insights[:MaxInsights]
		}
	}
	return nil
}

func (s *Store) saveLocked() error {
	rec := persistState{
		Insights:  s.insights,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write hermes-insight state: %w", err)
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) Append(insight HermesInsight) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.insights = append([]HermesInsight{insight}, s.insights...)
	if len(s.insights) > MaxInsights {
		s.insights = s.insights[:MaxInsights]
	}
	return s.saveLocked()
}

// List returns newest-first insights, capped at limit. total is the full ring size.
func (s *Store) List(limit int) ([]HermesInsight, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := len(s.insights)
	if limit <= 0 || limit > total {
		limit = total
	}
	out := make([]HermesInsight, limit)
	copy(out, s.insights[:limit])
	return out, total
}
