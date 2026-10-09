package patrol

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
)

// Store persists enable overlays and a 200-run ring buffer as JSON.
//
// Reads go back to the statefile every call. Writes use statefile.Update, so
// two processes (platform-api and platform-workers) apply their change to the
// latest copy instead of overwriting each other's whole document.
type Store struct {
	path string
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
	s := &Store{path: filepath.Join(dir, "state.json")}
	if _, err := s.read(); err != nil {
		return nil, err
	}
	return s, nil
}

func decode(data []byte) (persistState, error) {
	rec := persistState{Enabled: map[string]bool{}}
	if len(data) == 0 {
		return rec, nil
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		return persistState{}, fmt.Errorf("parse patrol state: %w", err)
	}
	if rec.Enabled == nil {
		rec.Enabled = map[string]bool{}
	}
	if len(rec.Runs) > MaxRuns {
		rec.Runs = rec.Runs[:MaxRuns]
	}
	return rec, nil
}

func encode(rec persistState) ([]byte, error) {
	rec.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return json.MarshalIndent(rec, "", "  ")
}

func (s *Store) read() (persistState, error) {
	data, err := statefile.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return persistState{Enabled: map[string]bool{}}, nil
		}
		return persistState{}, fmt.Errorf("read patrol state: %w", err)
	}
	return decode(data)
}

func (s *Store) edit(fn func(*persistState) error) error {
	return statefile.Update(s.path, func(old []byte) ([]byte, error) {
		rec, err := decode(old)
		if err != nil {
			return nil, err
		}
		if err := fn(&rec); err != nil {
			return nil, err
		}
		if len(rec.Runs) > MaxRuns {
			rec.Runs = rec.Runs[:MaxRuns]
		}
		return encode(rec)
	})
}

func (s *Store) Enabled(id string, yamlDefault bool) bool {
	rec, err := s.read()
	if err != nil {
		return yamlDefault
	}
	if v, ok := rec.Enabled[id]; ok {
		return v
	}
	return yamlDefault
}

func (s *Store) SetEnabled(id string, enabled bool) error {
	return s.edit(func(rec *persistState) error {
		rec.Enabled[id] = enabled
		return nil
	})
}

func (s *Store) AppendRun(run PatrolRun) error {
	return s.edit(func(rec *persistState) error {
		rec.Runs = append([]PatrolRun{run}, rec.Runs...)
		return nil
	})
}

func (s *Store) UpdateRun(id string, mutate func(*PatrolRun)) error {
	return s.edit(func(rec *persistState) error {
		for i := range rec.Runs {
			if rec.Runs[i].ID != id {
				continue
			}
			mutate(&rec.Runs[i])
			return nil
		}
		return fmt.Errorf("patrol run %s not found", id)
	})
}

func (s *Store) ListRuns(limit int) ([]PatrolRun, int) {
	rec, err := s.read()
	if err != nil {
		return nil, 0
	}
	total := len(rec.Runs)
	if limit <= 0 || limit > total {
		limit = total
	}
	out := make([]PatrolRun, limit)
	copy(out, rec.Runs[:limit])
	return out, total
}

func (s *Store) LastRun(skillID string) *PatrolRun {
	rec, err := s.read()
	if err != nil {
		return nil
	}
	for i := range rec.Runs {
		if rec.Runs[i].SkillID == skillID {
			cp := rec.Runs[i]
			return &cp
		}
	}
	return nil
}
