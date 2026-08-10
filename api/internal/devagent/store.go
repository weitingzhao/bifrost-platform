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

type PhaseSignOffRecord struct {
	PhaseID     string `json:"phase_id"`
	SignedOffAt string `json:"signed_off_at"`
	SignedOffBy string `json:"signed_off_by,omitempty"`
	Notes       string `json:"notes,omitempty"`
}

type PhaseProgressRecord struct {
	PhaseID      string `json:"phase_id"`
	Status       string `json:"status"`
	Summary      string `json:"summary,omitempty"`
	VerifyPassed bool   `json:"verify_passed"`
	UpdatedAt    string `json:"updated_at"`
}

type AgentSessionRecord struct {
	ID            string `json:"id"`
	PhaseID       string `json:"phase_id,omitempty"`
	ProgramID     string `json:"program_id,omitempty"`
	StartedAt     string `json:"started_at"`
	EndedAt       string `json:"ended_at,omitempty"`
	CursorAgentID string `json:"cursor_agent_id,omitempty"`
	Summary       string `json:"summary,omitempty"`
	Track         string `json:"track,omitempty"`
	Lane          string `json:"lane,omitempty"`
	Intent        string `json:"intent,omitempty"`
}

type PostCompletionItem struct {
	ID                 string   `json:"id"`
	ProgramID          string   `json:"program_id"`
	SourceLaneID       string   `json:"source_lane_id,omitempty"`
	OperateLane        string   `json:"operate_lane,omitempty"`
	Title              string   `json:"title"`
	Description        string   `json:"description,omitempty"`
	HandoffKind        string   `json:"handoff_kind,omitempty"`
	Reason             string   `json:"reason,omitempty"`
	AgentTaskID        string   `json:"agent_task_id,omitempty"`
	AcceptanceCriteria []string `json:"acceptance_criteria,omitempty"`
	VerificationSteps  []string `json:"verification_steps,omitempty"`
	RiskLevel          string   `json:"risk_level,omitempty"`
	Owner              string   `json:"owner,omitempty"`
	DueAt              string   `json:"due_at,omitempty"`
	ExecutionJobID     string   `json:"execution_job_id,omitempty"`
	CompletionEvidence []string `json:"completion_evidence,omitempty"`
	Status             string   `json:"status"` // pending_review | approved | rejected | in_operate | closed
	CreatedAt          string   `json:"created_at"`
	ApprovedAt         string   `json:"approved_at,omitempty"`
	ApprovedBy         string   `json:"approved_by,omitempty"`
	RejectedAt         string   `json:"rejected_at,omitempty"`
	RejectedBy         string   `json:"rejected_by,omitempty"`
	DecisionNote       string   `json:"decision_note,omitempty"`
}

type ProgramStateRecord struct {
	Version        string                `json:"version"`
	ProgramID      string                `json:"program_id"`
	LaneID         string                `json:"lane_id,omitempty"`
	Phases         []Phase               `json:"phases"`
	ActiveJob      *Job                  `json:"active_job"`
	History        []Job                 `json:"history"`
	PhaseSignOffs  []PhaseSignOffRecord  `json:"phase_sign_offs,omitempty"`
	PhaseProgress  []PhaseProgressRecord `json:"phase_progress,omitempty"`
	AgentSessions  []AgentSessionRecord  `json:"agent_sessions,omitempty"`
	PostCompletion *PostCompletionState  `json:"post_completion,omitempty"`
	UpdatedAt      string                `json:"updated_at"`
}

type PostCompletionState struct {
	SubmittedAt         string                      `json:"submitted_at,omitempty"`
	AssessedAt          string                      `json:"assessed_at,omitempty"`
	AssessedBy          string                      `json:"assessed_by,omitempty"`
	AssessmentStatus    string                      `json:"assessment_status,omitempty"`
	NoHandoffReason     string                      `json:"no_handoff_reason,omitempty"`
	SuggestedAssessment string                      `json:"suggested_assessment,omitempty"`
	NewCapabilities     []string                    `json:"new_capabilities,omitempty"`
	NewRisks            []string                    `json:"new_risks,omitempty"`
	SuggestedItems      []OperateQueueItemBlueprint `json:"suggested_items,omitempty"`
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
	dir := filepath.Join(dataDir, "programs")
	legacy := filepath.Join(dataDir, "dev-agent")
	if entries, err := os.ReadDir(legacy); err == nil && len(entries) > 0 {
		if _, err := os.ReadDir(dir); os.IsNotExist(err) {
			_ = os.Rename(legacy, dir)
		}
	}
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

func (s *FileStore) SaveProgramRecord(rec *ProgramStateRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveProgramRecordLocked(rec)
}

func (s *FileStore) saveProgramRecordLocked(rec *ProgramStateRecord) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("mkdir programs state: %w", err)
	}
	if rec.History == nil {
		rec.History = []Job{}
	}
	rec.Version = stateVersion
	rec.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	path := s.programPath(rec.ProgramID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write program state: %w", err)
	}
	return os.Rename(tmp, path)
}

func (s *FileStore) SaveProgram(programID string, phases []Phase, activeJob *Job, history []Job) error {
	rec, err := s.LoadProgram(programID)
	if err != nil {
		return err
	}
	if rec == nil {
		rec = &ProgramStateRecord{ProgramID: programID}
	}
	rec.Phases = phases
	rec.ActiveJob = activeJob
	rec.History = history
	return s.SaveProgramRecord(rec)
}

func (s *FileStore) LoadOrCreateProgram(programID string) (*ProgramStateRecord, error) {
	rec, err := s.LoadProgram(programID)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return &ProgramStateRecord{ProgramID: programID, History: []Job{}}, nil
	}
	return rec, nil
}

func (s *FileStore) PendingPostCompletionPath() string {
	return filepath.Join(s.dir, "_post_completion_pending.json")
}

func (s *FileStore) LoadPendingPostCompletion() ([]PostCompletionItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.PendingPostCompletionPath())
	if err != nil {
		if os.IsNotExist(err) {
			return []PostCompletionItem{}, nil
		}
		return nil, err
	}
	var items []PostCompletionItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}
	if items == nil {
		return []PostCompletionItem{}, nil
	}
	for i := range items {
		if items[i].HandoffKind == "" {
			items[i].HandoffKind = "one_off"
		}
		if items[i].RiskLevel == "" {
			items[i].RiskLevel = "low"
		}
	}
	return items, nil
}

func (s *FileStore) SavePendingPostCompletion(items []PostCompletionItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	if items == nil {
		items = []PostCompletionItem{}
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.PendingPostCompletionPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.PendingPostCompletionPath())
}

func (s *FileStore) ListInfo(activeProgramID string, includeArchived bool, activeIDs map[string]struct{}) (*PersistenceInfo, error) {
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
		if !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, "_") {
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
			if rec.ProgramID != "" {
				programID = rec.ProgramID
			}
		}
		if !includeArchived {
			if _, ok := activeIDs[programID]; !ok {
				continue
			}
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
		id := canonicalPhaseID(blueprint, p.ID)
		p.ID = id
		byID[id] = p
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
