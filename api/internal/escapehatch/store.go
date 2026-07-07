package escapehatch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const storeVersion = "2026-07-07"
const quarterlyIntervalDays = 90

type Store struct {
	path string
	mu   sync.Mutex
}

func NewStore(configDir string) *Store {
	dataDir := os.Getenv("PLATFORM_DATA_DIR")
	if dataDir == "" {
		dataDir = filepath.Join(configDir, "..", "data")
	}
	dir := filepath.Join(dataDir, "escape_hatch")
	return &Store{path: filepath.Join(dir, "last_drill.json")}
}

func (s *Store) Path() string {
	return s.path
}

func (s *Store) LoadDrill() (*DrillRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read escape hatch drill: %w", err)
	}
	var rec DrillRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("parse escape hatch drill: %w", err)
	}
	return &rec, nil
}

func (s *Store) SaveDrill(rec DrillRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir escape hatch: %w", err)
	}
	rec.Version = storeVersion
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write escape hatch drill: %w", err)
	}
	return os.Rename(tmp, s.path)
}

func quarterlyFromRecord(rec *DrillRecord) QuarterlyDrill {
	q := QuarterlyDrill{IntervalDays: quarterlyIntervalDays}
	if rec == nil || rec.At.IsZero() {
		q.Overdue = true
		return q
	}
	at := rec.At.UTC()
	q.LastDrillAt = &at
	q.LastDrillBy = rec.By
	q.Notes = rec.Notes
	days := int(time.Since(at).Hours() / 24)
	q.DaysSince = &days
	next := at.AddDate(0, 0, quarterlyIntervalDays)
	q.NextDueAt = &next
	q.Overdue = time.Now().UTC().After(next)
	return q
}
