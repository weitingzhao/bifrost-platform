package agentprogress

import (
	"encoding/json"
	"errors"
	"io/fs"
	"sync"

	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
)

// Store is the statefile key that holds the one snapshot. An empty path keeps
// it in memory (tests, and a process without a data directory).
type Store struct {
	path string
	mu   sync.Mutex
	mem  []byte
}

func NewStore(path string) *Store { return &Store{path: path} }

func (s *Store) Save(snap Snapshot) error {
	raw, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	if s.path == "" {
		s.mu.Lock()
		s.mem = append([]byte(nil), raw...)
		s.mu.Unlock()
		return nil
	}
	return statefile.WriteFile(s.path, raw, 0o644)
}

// Load reads the snapshot. A missing file is ok=false and a nil error.
func (s *Store) Load() (Snapshot, bool, error) {
	raw, err := s.read()
	if err != nil {
		return Snapshot{}, false, err
	}
	if len(raw) == 0 {
		return Snapshot{}, false, nil
	}
	var snap Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return Snapshot{}, false, err
	}
	if len(snap.Document) == 0 || string(snap.Document) == "null" {
		return Snapshot{}, false, nil
	}
	snap.Document = append(json.RawMessage(nil), snap.Document...)
	return snap, true, nil
}

func (s *Store) read() ([]byte, error) {
	if s.path == "" {
		s.mu.Lock()
		defer s.mu.Unlock()
		if len(s.mem) == 0 {
			return nil, nil
		}
		return append([]byte(nil), s.mem...), nil
	}
	raw, err := statefile.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return raw, nil
}
