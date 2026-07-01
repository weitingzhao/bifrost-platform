package briefing

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type SessionResult struct {
	ID        string    `json:"id"`
	ClosedAt  time.Time `json:"closed_at"`
	ClosedBy  string    `json:"closed_by"`
	JobID     string    `json:"job_id,omitempty"`
	Outcome   string    `json:"outcome"`
	Summary   string    `json:"summary"`
	Track     string    `json:"track,omitempty"`
	Lane      string    `json:"lane,omitempty"`
	Intent    string    `json:"intent,omitempty"`
	SpineNote string    `json:"spine_note,omitempty"`
}

type SessionResultStore struct {
	mu   sync.Mutex
	path string
}

func NewSessionResultStore() *SessionResultStore {
	p := os.Getenv("PLATFORM_BRIEFING_SESSION_RESULTS_PATH")
	if p == "" {
		dir := os.Getenv("PLATFORM_DATA_DIR")
		if dir == "" {
			dir = filepath.Join(os.Getenv("HOME"), ".bifrost-platform", "data")
		}
		p = filepath.Join(dir, "briefing-session-results.json")
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	return &SessionResultStore{path: p}
}

func (s *SessionResultStore) Append(r SessionResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.loadLocked()
	list = append([]SessionResult{r}, list...)
	if len(list) > 100 {
		list = list[:100]
	}
	raw, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, raw, 0o644)
}

func (s *SessionResultStore) List(limit int) []SessionResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.loadLocked()
	if limit > 0 && len(list) > limit {
		return list[:limit]
	}
	return list
}

func (s *SessionResultStore) loadLocked() []SessionResult {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return nil
	}
	var list []SessionResult
	if json.Unmarshal(raw, &list) != nil {
		return nil
	}
	return list
}

func newSessionResultID() string {
	return fmt.Sprintf("bsr-%d", time.Now().UTC().UnixNano())
}
