package vision

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Store struct {
	gatePath    string
	signoffPath string
	mu          sync.RWMutex
}

func NewStore(configDir string) *Store {
	base := os.Getenv("PLATFORM_VISION_V1_STATE")
	if base == "" {
		if configDir != "" {
			base = filepath.Join(configDir, "vision_v1_gate.json")
		} else {
			base = "config/vision_v1_gate.json"
		}
	}
	signoff := stringsTrimSuffixExt(base) + "_signoff.json"
	if filepath.IsAbs(base) || strings.Contains(base, string(filepath.Separator)) {
		signoff = filepath.Join(filepath.Dir(base), filepath.Base(signoff))
	}
	return &Store{gatePath: base, signoffPath: signoff}
}

func stringsTrimSuffixExt(path string) string {
	return path[:len(path)-len(filepath.Ext(path))]
}

type GateRecord struct {
	At          time.Time  `json:"at"`
	Result      string     `json:"result"`
	Checks      []GateCheck `json:"checks"`
	TriggeredBy string     `json:"triggered_by,omitempty"`
	Summary     string     `json:"summary,omitempty"`
}

func (s *Store) LoadGate() (*GateRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, err := os.ReadFile(s.gatePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read vision v1 gate: %w", err)
	}
	var rec GateRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("parse vision v1 gate: %w", err)
	}
	return &rec, nil
}

func (s *Store) SaveGate(rec GateRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.gatePath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := s.gatePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.gatePath)
}

func (s *Store) LoadSignoff() (*V1SignoffRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, err := os.ReadFile(s.signoffPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read vision v1 signoff: %w", err)
	}
	var rec V1SignoffRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("parse vision v1 signoff: %w", err)
	}
	return &rec, nil
}

func (s *Store) SaveSignoff(rec V1SignoffRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.signoffPath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := s.signoffPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.signoffPath)
}
