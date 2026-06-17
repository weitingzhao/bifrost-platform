package promote

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Store struct {
	path string
	mu   sync.RWMutex
}

func NewStore(configDir string) *Store {
	path := os.Getenv("PLATFORM_RELEASE_GATE_STATE")
	if path == "" {
		if configDir != "" {
			path = filepath.Join(configDir, "release_gate_state.json")
		} else {
			path = "config/release_gate_state.json"
		}
	}
	return &Store{path: path}
}

func (s *Store) Path() string {
	return s.path
}

func (s *Store) Load() (*ReleaseGateRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read release gate state: %w", err)
	}
	var rec ReleaseGateRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("parse release gate state: %w", err)
	}
	return &rec, nil
}

func (s *Store) Save(rec ReleaseGateRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("mkdir release gate state dir: %w", err)
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write release gate state: %w", err)
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) AppendLog(line string) error {
	logPath := os.Getenv("PLATFORM_RELEASE_GATE_LOG")
	if logPath == "" {
		logPath = filepath.Join(filepath.Dir(s.path), "release_gate.log")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "[%s] %s\n", time.Now().UTC().Format(time.RFC3339), line)
	return err
}
