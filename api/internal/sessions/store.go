package sessions

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Store struct {
	dir string
	mu  sync.Mutex
}

func NewStore(configDir string) *Store {
	dataDir := os.Getenv("PLATFORM_DATA_DIR")
	if dataDir == "" {
		dataDir = filepath.Join(configDir, "..", "data")
	}
	dir := filepath.Join(dataDir, "sessions")
	_ = os.MkdirAll(dir, 0o755)
	return &Store{dir: dir}
}

func (s *Store) Dir() string { return s.dir }

func HashPack(pack string) string {
	sum := sha256.Sum256([]byte(pack))
	return hex.EncodeToString(sum[:])
}

func (s *Store) pathFor(id string) string {
	return filepath.Join(s.dir, id+".json")
}

func (s *Store) Create(req CreateRequest) (Record, error) {
	programID := strings.TrimSpace(req.ProgramID)
	phaseID := strings.TrimSpace(req.PhaseID)
	if programID == "" || phaseID == "" {
		return Record{}, fmt.Errorf("program_id and phase_id required")
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		sessionID = uuid.NewString()
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = StatusOpen
	}
	packHash := strings.TrimSpace(req.PackHash)
	if packHash == "" && strings.TrimSpace(req.Pack) != "" {
		packHash = HashPack(req.Pack)
	}

	rec := Record{
		SessionID:     sessionID,
		ProgramID:     programID,
		PhaseID:       phaseID,
		LaneID:        strings.TrimSpace(req.LaneID),
		PackHash:      packHash,
		Status:        status,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		CursorAgentID: strings.TrimSpace(req.CursorAgentID),
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.pathFor(sessionID)
	if _, err := os.Stat(path); err == nil {
		return Record{}, fmt.Errorf("session already exists: %s", sessionID)
	}
	if err := s.writeLocked(rec); err != nil {
		return Record{}, err
	}
	return rec, nil
}

func (s *Store) writeLocked(rec Record) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	path := s.pathFor(rec.SessionID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Store) Get(id string) (Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.pathFor(strings.TrimSpace(id)))
	if err != nil {
		if os.IsNotExist(err) {
			return Record{}, false, nil
		}
		return Record{}, false, err
	}
	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil {
		return Record{}, false, err
	}
	return rec, true, nil
}

func (s *Store) List(limit int) ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Record{}, nil
		}
		return nil, err
	}
	var out []Record
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var rec Record
		if json.Unmarshal(data, &rec) != nil {
			continue
		}
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt > out[j].CreatedAt
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ValidateProgressHook requires a non-empty session_id that matches the archived
// session's program/phase. Empty sessionID is rejected (phase progress must be
 // anchored to a Session Job).
func (s *Store) ValidateProgressHook(sessionID, programID, phaseID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("session_id required for phase progress reporting")
	}
	rec, ok, err := s.Get(sessionID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	if rec.ProgramID != strings.TrimSpace(programID) {
		return fmt.Errorf("session program_id mismatch: want %s got %s", rec.ProgramID, programID)
	}
	if rec.PhaseID != strings.TrimSpace(phaseID) {
		return fmt.Errorf("session phase_id mismatch: want %s got %s", rec.PhaseID, phaseID)
	}
	return nil
}
