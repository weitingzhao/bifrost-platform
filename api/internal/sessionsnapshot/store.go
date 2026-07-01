package sessionsnapshot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

const latestFileName = "latest.json"

// Envelope wraps the client SessionSnapshot payload with server metadata.
type Envelope struct {
	SavedAt string          `json:"saved_at"`
	SavedBy string          `json:"saved_by"`
	Payload json.RawMessage `json:"payload"`
}

type Store struct {
	mu   sync.Mutex
	path string
}

func NewStore() *Store {
	p := os.Getenv("PLATFORM_SESSION_SNAPSHOT_PATH")
	if p == "" {
		dir := os.Getenv("PLATFORM_DATA_DIR")
		if dir == "" {
			dir = filepath.Join(os.Getenv("HOME"), ".bifrost-platform", "data")
		}
		p = filepath.Join(dir, "session-snapshots", latestFileName)
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	return &Store{path: p}
}

func (s *Store) Save(env Envelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, raw, 0o644)
}

func (s *Store) Latest() (*Envelope, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return nil, false
	}
	var env Envelope
	if json.Unmarshal(raw, &env) != nil {
		return nil, false
	}
	return &env, true
}
