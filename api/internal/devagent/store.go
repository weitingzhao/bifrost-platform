package devagent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const stateVersion = "2026-07-04"

type ProgramStateRecord struct {
	Version   string  `json:"version"`
	ProgramID string  `json:"program_id"`
	Phases    []Phase `json:"phases"`
	ActiveJob *Job    `json:"active_job"`
	History   []Job   `json:"history"`
	UpdatedAt string  `json:"updated_at"`
}

type ActiveProgramRecord struct {
	ActiveProgramID string `json:"active_program_id"`
	UpdatedAt       string `json:"updated_at"`
}

type PersistenceFileInfo struct {
	ProgramID string `json:"program_id"`
	Path      string `json:"path"`
	UpdatedAt string `json:"updated_at,omitempty"`
	Bytes     int    `json:"bytes"`
}

type PersistenceInfo struct {
	StateDir          string                `json:"state_dir"`
	ActiveProgramID   string                `json:"active_program_id"`
	ActiveProgramPath string                `json:"active_program_path"`
	Files             []PersistenceFileInfo `json:"files"`
}

type FileStore struct {
	dir string
	mu  sync.Mutex
}

func NewFileStore(configDir string) *FileStore {
	dataDir := os.Getenv("PLATFORM_DATA_DIR")
	if dataDir == "" {
		dataDir = filepath.Join(configDir, "..", "data")
	}
	dir := filepath.Join(dataDir, "dev-agent")
	return &FileStore{dir: dir}
}

func (s *FileStore) Dir() string {
	return s.dir
}

func (s *FileStore) activePath() string {
	return filepath.Join(s.dir, "_active.json")
}

func (s *FileStore) programPath(programID string) string {
	safe := strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(programID)
	return filepath.Join(s.dir, safe+".json")
}

func (s *FileStore) LoadActiveProgramID() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.activePath())
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read active program state: %w", err)
	}
	var rec ActiveProgramRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return "", fmt.Errorf("parse active program state: %w", err)
	}
	return rec.ActiveProgramID, nil
}

func (s *FileStore) SaveActiveProgramID(programID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("mkdir dev-agent state: %w", err)
	}
	rec := ActiveProgramRecord{
		ActiveProgramID: programID,
		UpdatedAt:       time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.activePath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write active program state: %w", err)
	}
	return os.Rename(tmp, s.activePath())
}

func (s *FileStore) LoadProgram(programID string) (*ProgramStateRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadProgramLocked(programID)
}

func (s *FileStore) loadProgramLocked(programID string) (*ProgramStateRecord, error) {
	data, err := os.ReadFile(s.programPath(programID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read program state %s: %w", programID, err)
	}
	var rec ProgramStateRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("parse program state %s: %w", programID, err)
	}
	return &rec, nil
}

func (s *FileStore) SaveProgram(programID string, phases []Phase, activeJob *Job, history []Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("mkdir dev-agent state: %w", err)
	}
	if history == nil {
		history = []Job{}
	}
	rec := ProgramStateRecord{
		Version:   stateVersion,
		ProgramID: programID,
		Phases:    phases,
		ActiveJob: activeJob,
		History:   history,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	path := s.programPath(programID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write program state: %w", err)
	}
	return os.Rename(tmp, path)
}

func (s *FileStore) ListInfo(activeProgramID string) (*PersistenceInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	info := &PersistenceInfo{
		StateDir:          s.dir,
		ActiveProgramID:   activeProgramID,
		ActiveProgramPath: s.activePath(),
		Files:             []PersistenceFileInfo{},
	}

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return info, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") || name == "_active.json" {
			continue
		}
		path := filepath.Join(s.dir, name)
		st, err := entry.Info()
		if err != nil {
			continue
		}
		programID := strings.TrimSuffix(name, ".json")
		updatedAt := ""
		if rec, err := s.loadProgramLocked(programID); err == nil && rec != nil {
			updatedAt = rec.UpdatedAt
			programID = rec.ProgramID
		}
		info.Files = append(info.Files, PersistenceFileInfo{
			ProgramID: programID,
			Path:      path,
			UpdatedAt: updatedAt,
			Bytes:     int(st.Size()),
		})
	}
	return info, nil
}

func mergePhasesFromState(blueprint *ProgramBlueprint, saved []Phase) []Phase {
	base := phasesFromBlueprint(blueprint)
	if len(saved) == 0 {
		return base
	}
	byID := make(map[string]Phase, len(saved))
	for _, p := range saved {
		byID[p.ID] = p
	}
	for i := range base {
		if savedPhase, ok := byID[base[i].ID]; ok {
			base[i].Status = savedPhase.Status
			base[i].StartedAt = savedPhase.StartedAt
			base[i].CompletedAt = savedPhase.CompletedAt
		}
	}
	return base
}
