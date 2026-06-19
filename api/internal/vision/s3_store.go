package vision

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type S3Store struct {
	gatePath    string
	signoffPath string
	mu          sync.RWMutex
}

func NewS3Store(configDir string) *S3Store {
	base := os.Getenv("PLATFORM_VISION_S3_STATE")
	if base == "" {
		if configDir != "" {
			base = filepath.Join(configDir, "vision_s3_gate.json")
		} else {
			base = "config/vision_s3_gate.json"
		}
	}
	signoff := stringsTrimSuffixExt(base) + "_signoff.json"
	if filepath.IsAbs(base) || stringsContainsSep(base) {
		signoff = filepath.Join(filepath.Dir(base), filepath.Base(signoff))
	}
	return &S3Store{gatePath: base, signoffPath: signoff}
}

func stringsContainsSep(path string) bool {
	return filepath.Dir(path) != "."
}

func (s *S3Store) LoadGate() (*GateRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, err := os.ReadFile(s.gatePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read vision s3 gate: %w", err)
	}
	var rec GateRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("parse vision s3 gate: %w", err)
	}
	return &rec, nil
}

func (s *S3Store) SaveGate(rec GateRecord) error {
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

func (s *S3Store) LoadSignoff() (*V1SignoffRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, err := os.ReadFile(s.signoffPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read vision s3 signoff: %w", err)
	}
	var rec V1SignoffRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("parse vision s3 signoff: %w", err)
	}
	return &rec, nil
}

func (s *S3Store) SaveSignoff(rec V1SignoffRecord) error {
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
