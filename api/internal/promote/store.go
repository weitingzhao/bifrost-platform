package promote

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
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

func (s *Store) LoadTier(tier GateTier) (*ReleaseGateRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadTierLocked(tier)
}

func (s *Store) loadTierLocked(tier GateTier) (*ReleaseGateRecord, error) {
	path := s.tierPath(tier)
	data, err := statefile.ReadFile(path)
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
	if err := statefile.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write release gate state: %w", err)
	}
	return nil
}

func (s *Store) Save(rec ReleaseGateRecord) error {
	if rec.Tier == GateTierStg {
		return s.SaveTier(GateTierStg, rec)
	}
	return s.SaveTier(GateTierProd, rec)
}
