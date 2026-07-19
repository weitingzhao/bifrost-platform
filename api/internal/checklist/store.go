package checklist

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const historyLimit = 30

type Store struct {
	path string
	mu   sync.Mutex
}

func NewStore(configDir string) *Store {
	dataDir := os.Getenv("PLATFORM_DATA_DIR")
	if dataDir == "" {
		dataDir = filepath.Join(configDir, "..", "data")
	}
	dir := filepath.Join(dataDir, "checklist")
	return &Store{path: filepath.Join(dir, "signals.json")}
}

func (s *Store) Path() string { return s.path }

func (s *Store) loadLocked() (*FileRecord, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return &FileRecord{Version: stateVersion, Signals: []ItemSignal{}}, nil
		}
		return nil, fmt.Errorf("read checklist signals: %w", err)
	}
	var rec FileRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("parse checklist signals: %w", err)
	}
	if rec.Signals == nil {
		rec.Signals = []ItemSignal{}
	}
	if rec.Version == "" {
		rec.Version = stateVersion
	}
	return &rec, nil
}

func (s *Store) saveLocked(rec *FileRecord) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir checklist: %w", err)
	}
	rec.Version = stateVersion
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write checklist: %w", err)
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) Get() (SignalsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadLocked()
	if err != nil {
		return SignalsResponse{}, err
	}
	return toResponse(rec, nil), nil
}

func (s *Store) KPIs() (KPIResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadLocked()
	if err != nil {
		return KPIResponse{}, err
	}
	var last *RunSummary
	if len(rec.History) > 0 {
		h := rec.History[len(rec.History)-1]
		last = &h
	}
	hint := ""
	if rec.QuietSuccessStreak == 0 && rec.LastFailAt != "" {
		hint = "New or ongoing checklist failures since last quiet streak — review Checklist Action column."
	}
	hist := rec.History
	if len(hist) > 10 {
		hist = hist[len(hist)-10:]
	}
	return KPIResponse{
		QuietSuccessStreak: rec.QuietSuccessStreak,
		LastRunID:          rec.LastRunID,
		UpdatedAt:          rec.UpdatedAt,
		LastFailAt:         rec.LastFailAt,
		LastAllOkAt:        rec.LastAllOkAt,
		LastCounts:         last,
		RecentHistory:      hist,
		NewFailHint:        hint,
	}, nil
}

// Merge replaces/merges signals by item_id and updates streak KPIs.
func (s *Store) Merge(req MergeRequest) (SignalsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, err := s.loadLocked()
	if err != nil {
		return SignalsResponse{}, err
	}

	prevFail := map[string]bool{}
	for _, sig := range rec.Signals {
		if sig.Signal == SignalFail || sig.Signal == SignalDegraded {
			prevFail[sig.ItemID] = true
		}
	}

	byID := map[string]ItemSignal{}
	for _, sig := range rec.Signals {
		byID[sig.ItemID] = sig
	}
	for _, sig := range req.Signals {
		id := strings.TrimSpace(sig.ItemID)
		if id == "" {
			continue
		}
		sig.ItemID = id
		sig.Signal = normalizeSignal(sig.Signal)
		sig.Detail = strings.TrimSpace(sig.Detail)
		sig.Env = strings.TrimSpace(sig.Env)
		byID[id] = sig
	}
	merged := make([]ItemSignal, 0, len(byID))
	for _, sig := range byID {
		merged = append(merged, sig)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	ok, fail, unknown := 0, 0, 0
	allOk := true
	var newFailures []string
	for _, sig := range merged {
		switch sig.Signal {
		case SignalOK:
			ok++
		case SignalFail, SignalDegraded:
			fail++
			allOk = false
			if !prevFail[sig.ItemID] {
				newFailures = append(newFailures, sig.ItemID)
			}
		default:
			unknown++
			if sig.Signal != SignalOK {
				allOk = false
			}
		}
	}
	// unknown alone does not break quiet success if zero fail/degraded
	quietOk := fail == 0

	rec.Signals = merged
	rec.UpdatedAt = now
	rec.LastRunID = strings.TrimSpace(req.RunID)
	rec.Source = strings.TrimSpace(req.Source)
	if quietOk {
		rec.QuietSuccessStreak++
		rec.LastAllOkAt = now
	} else {
		rec.QuietSuccessStreak = 0
		rec.LastFailAt = now
	}

	summary := RunSummary{
		At: now, RunID: rec.LastRunID, Ok: ok, Fail: fail, Unknown: unknown, AllOk: allOk && quietOk,
	}
	rec.History = append(rec.History, summary)
	if len(rec.History) > historyLimit {
		rec.History = rec.History[len(rec.History)-historyLimit:]
	}

	if err := s.saveLocked(rec); err != nil {
		return SignalsResponse{}, err
	}
	return toResponse(rec, newFailures), nil
}

func (s *Store) SetDispatch(actions []DispatchAction) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadLocked()
	if err != nil {
		return err
	}
	rec.LastDispatch = actions
	rec.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return s.saveLocked(rec)
}

func toResponse(rec *FileRecord, newFailures []string) SignalsResponse {
	if newFailures == nil {
		newFailures = []string{}
	}
	return SignalsResponse{
		UpdatedAt:          rec.UpdatedAt,
		LastRunID:          rec.LastRunID,
		Source:             rec.Source,
		Signals:            rec.Signals,
		LastDispatch:       rec.LastDispatch,
		QuietSuccessStreak: rec.QuietSuccessStreak,
		LastFailAt:         rec.LastFailAt,
		LastAllOkAt:        rec.LastAllOkAt,
		NewFailures:        newFailures,
	}
}

func normalizeSignal(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case SignalOK, "pass", "green":
		return SignalOK
	case SignalDegraded, "warn", "warning":
		return SignalDegraded
	case SignalFail, "red", "error":
		return SignalFail
	default:
		return SignalUnknown
	}
}
