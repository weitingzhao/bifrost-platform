package operatequeue

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const recentClosedLimit = 20

type Store struct {
	path string
	mu   sync.Mutex
}

func NewStore(configDir string) *Store {
	dataDir := os.Getenv("PLATFORM_DATA_DIR")
	if dataDir == "" {
		dataDir = filepath.Join(configDir, "..", "data")
	}
	dir := filepath.Join(dataDir, "operate")
	return &Store{path: filepath.Join(dir, "queue.json")}
}

func (s *Store) Path() string {
	return s.path
}

func (s *Store) loadLocked() (*FileRecord, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return &FileRecord{Version: stateVersion, Items: []Item{}}, nil
		}
		return nil, fmt.Errorf("read operate queue: %w", err)
	}
	var rec FileRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("parse operate queue: %w", err)
	}
	if rec.Items == nil {
		rec.Items = []Item{}
	}
	if rec.Version == "" {
		rec.Version = stateVersion
	}
	return &rec, nil
}

func (s *Store) saveLocked(rec *FileRecord) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir operate queue: %w", err)
	}
	rec.Version = stateVersion
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write operate queue: %w", err)
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) List() (ListResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, err := s.loadLocked()
	if err != nil {
		return ListResponse{}, err
	}

	var open []Item
	var closed []Item
	for _, item := range rec.Items {
		switch item.Status {
		case StatusOpen:
			open = append(open, item)
		case StatusClosed:
			closed = append(closed, item)
		}
	}
	if open == nil {
		open = []Item{}
	}
	if len(closed) > recentClosedLimit {
		closed = closed[len(closed)-recentClosedLimit:]
	}
	if closed == nil {
		closed = []Item{}
	}
	return ListResponse{Open: open, RecentClosed: closed}, nil
}

func (s *Store) FindByPendingID(pendingID string) (*Item, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, err := s.loadLocked()
	if err != nil {
		return nil, false
	}
	for i := range rec.Items {
		if rec.Items[i].PendingID == pendingID {
			return &rec.Items[i], true
		}
	}
	return nil, false
}

func (s *Store) Add(item Item) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, err := s.loadLocked()
	if err != nil {
		return Item{}, err
	}
	if item.PendingID != "" {
		for _, existing := range rec.Items {
			if existing.PendingID == item.PendingID {
				return existing, nil
			}
		}
	}
	rec.Items = append(rec.Items, item)
	if err := s.saveLocked(rec); err != nil {
		return Item{}, err
	}
	return item, nil
}

func NewItemFromApproval(params ApprovalInjectParams) Item {
	now := time.Now().UTC().Format(time.RFC3339)
	lane := strings.TrimSpace(params.Lane)
	if lane != "" && !ValidLanes[lane] {
		lane = ""
	}
	return Item{
		ID:          uuid.New().String(),
		ProgramID:   params.ProgramID,
		Lane:        lane,
		Title:       params.Title,
		Description: params.Description,
		Status:      StatusOpen,
		CreatedAt:   now,
		UpdatedAt:   now,
		Source:      SourcePostCompletion,
		PendingID:   params.PendingID,
		ApprovedBy:  params.ApprovedBy,
	}
}

func NewItemFromManual(req EnqueueRequest) (Item, error) {
	title := strings.TrimSpace(req.Title)
	programID := strings.TrimSpace(req.ProgramID)
	if title == "" {
		return Item{}, fmt.Errorf("title required")
	}
	if programID == "" {
		return Item{}, fmt.Errorf("program_id required")
	}
	lane := strings.TrimSpace(req.Lane)
	if lane != "" && !ValidLanes[lane] {
		return Item{}, fmt.Errorf("invalid lane")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	return Item{
		ID:          uuid.New().String(),
		ProgramID:   programID,
		Lane:        lane,
		Title:       title,
		Description: strings.TrimSpace(req.Description),
		Status:      StatusOpen,
		CreatedAt:   now,
		UpdatedAt:   now,
		Source:      SourceManual,
	}, nil
}
