package promote

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
	path string
	mu   sync.RWMutex
}

func NewStore(configDir string) *Store {
	path := os.Getenv("PLATFORM_RELEASE_GATE_STATE")
	if path == "" {
		dataDir := os.Getenv("PLATFORM_DATA_DIR")
		if dataDir == "" {
			dataDir = filepath.Join(configDir, "..", "data")
		}
		path = filepath.Join(dataDir, "release_gate_state.json")
	}
	return &Store{path: path}
}

func (s *Store) Path() string {
	return s.path
}

func (s *Store) tierPath(tier GateTier) string {
	base := strings.TrimSuffix(s.path, filepath.Ext(s.path))
	switch tier {
	case GateTierStg:
		return base + "_stg.json"
	case GateTierPlatformStg:
		return base + "_platform_stg.json"
	case GateTierPlatformProd:
		return base + "_platform_prod.json"
	default:
		return s.path
	}
}

func (s *Store) tierBPath() string {
	base := strings.TrimSuffix(s.path, filepath.Ext(s.path))
	return base + "_tier_b.json"
}

func (s *Store) LoadTier(tier GateTier) (*ReleaseGateRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadTierLocked(tier)
}

func (s *Store) loadTierLocked(tier GateTier) (*ReleaseGateRecord, error) {
	path := s.tierPath(tier)
	data, err := os.ReadFile(path)
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
	if rec.Tier == "" {
		rec.Tier = tier
	}
	return &rec, nil
}

// Load returns the prod-tier gate (backward compatible).
func (s *Store) Load() (*ReleaseGateRecord, error) {
	return s.LoadTier(GateTierProd)
}

func (s *Store) SaveTier(tier GateTier, rec ReleaseGateRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec.Tier = tier
	path := s.tierPath(tier)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir release gate state dir: %w", err)
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write release gate state: %w", err)
	}
	return os.Rename(tmp, path)
}

func (s *Store) Save(rec ReleaseGateRecord) error {
	if rec.Tier == GateTierStg {
		return s.SaveTier(GateTierStg, rec)
	}
	return s.SaveTier(GateTierProd, rec)
}

func (s *Store) LoadTierB() (*TierBSignoffRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	path := s.tierBPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read tier b signoff: %w", err)
	}
	var rec TierBSignoffRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("parse tier b signoff: %w", err)
	}
	return &rec, nil
}

func (s *Store) SaveTierB(rec TierBSignoffRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.tierBPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir tier b signoff dir: %w", err)
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tier b signoff: %w", err)
	}
	return os.Rename(tmp, path)
}

func (s *Store) historyPath(tier GateTier) string {
	base := strings.TrimSuffix(s.path, filepath.Ext(s.path))
	switch tier {
	case GateTierStg:
		return base + "_stg_history.json"
	case GateTierPlatformStg:
		return base + "_platform_stg_history.json"
	case GateTierPlatformProd:
		return base + "_platform_prod_history.json"
	default:
		return base + "_history.json"
	}
}

func (s *Store) AppendHistory(tier GateTier, rec ReleaseGateRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.historyPath(tier)
	var history []ReleaseGateRecord
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &history)
	}
	history = append(history, rec)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir gate history dir: %w", err)
	}
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write gate history: %w", err)
	}
	return os.Rename(tmp, path)
}

const maxHistoryEntries = 50

func (s *Store) LoadHistory(tier GateTier) ([]ReleaseGateRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	path := s.historyPath(tier)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read gate history: %w", err)
	}
	var history []ReleaseGateRecord
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, fmt.Errorf("parse gate history: %w", err)
	}
	if len(history) > maxHistoryEntries {
		history = history[len(history)-maxHistoryEntries:]
	}
	// Return in reverse chronological order.
	for i, j := 0, len(history)-1; i < j; i, j = i+1, j-1 {
		history[i], history[j] = history[j], history[i]
	}
	return history, nil
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
