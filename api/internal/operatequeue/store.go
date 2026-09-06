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
	"github.com/weitingzhao/bifrost-platform/api/internal/agentgovernance"
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
	for i := range rec.Items {
		normalizeLegacyItem(&rec.Items[i])
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

func (s *Store) FindByID(id string) (*Item, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadLocked()
	if err != nil {
		return nil, false
	}
	for i := range rec.Items {
		if rec.Items[i].ID == id {
			item := rec.Items[i]
			return &item, true
		}
	}
	return nil, false
}

func (s *Store) Close(id string, req CloseRequest, executionJobDone bool) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, err := s.loadLocked()
	if err != nil {
		return Item{}, err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return Item{}, fmt.Errorf("id required")
	}
	for i := range rec.Items {
		if rec.Items[i].ID != id {
			continue
		}
		if rec.Items[i].Status == StatusClosed {
			return rec.Items[i], nil
		}
		if rec.Items[i].Status != StatusOpen {
			return Item{}, fmt.Errorf("item not open")
		}
		evidence := cleanStrings(req.CompletionEvidence)
		if len(evidence) == 0 {
			return Item{}, fmt.Errorf("completion_evidence required")
		}
		if rec.Items[i].ExecutionJobID != "" && !executionJobDone {
			return Item{}, fmt.Errorf("execution job must be completed before close")
		}
		if rec.Items[i].ExecutionJobID != "" &&
			(!req.PostFixVerificationPassed || !hasEvidence(evidence, "post_fix_verification:passed")) {
			return Item{}, fmt.Errorf("post_fix_verification must pass before close")
		}
		if rec.Items[i].HandoffKind == HandoffRecurringSetup && !hasRecurringEvidence(evidence) {
			return Item{}, fmt.Errorf("recurring_setup requires schedule, skill, or operator setup evidence")
		}
		now := time.Now().UTC().Format(time.RFC3339)
		rec.Items[i].Status = StatusClosed
		rec.Items[i].CompletionEvidence = evidence
		rec.Items[i].UpdatedAt = now
		rec.Items[i].ClosedAt = now
		if err := s.saveLocked(rec); err != nil {
			return Item{}, err
		}
		return rec.Items[i], nil
	}
	return Item{}, fmt.Errorf("item not found")
}

// Dismiss soft-closes an open item when it is stale or already resolved outside
// the verified Close path. Still requires completion_evidence; does not require
// execution job done or post_fix_verification.
func (s *Store) Dismiss(id string, req DismissRequest) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, err := s.loadLocked()
	if err != nil {
		return Item{}, err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return Item{}, fmt.Errorf("id required")
	}
	evidence := cleanStrings(req.CompletionEvidence)
	if len(evidence) == 0 {
		return Item{}, fmt.Errorf("completion_evidence required")
	}
	reason := strings.ToLower(strings.TrimSpace(req.Reason))
	if reason == "" {
		reason = "stale"
	}
	if reason != "stale" && reason != "resolved" && reason != "other" {
		return Item{}, fmt.Errorf("reason must be stale, resolved, or other")
	}
	tag := "dismiss:" + reason
	if !hasEvidence(evidence, tag) {
		evidence = append([]string{tag}, evidence...)
	}
	for i := range rec.Items {
		if rec.Items[i].ID != id {
			continue
		}
		if rec.Items[i].Status == StatusClosed {
			return rec.Items[i], nil
		}
		if rec.Items[i].Status != StatusOpen {
			return Item{}, fmt.Errorf("item not open")
		}
		now := time.Now().UTC().Format(time.RFC3339)
		rec.Items[i].Status = StatusClosed
		rec.Items[i].CompletionEvidence = evidence
		rec.Items[i].UpdatedAt = now
		rec.Items[i].ClosedAt = now
		if err := s.saveLocked(rec); err != nil {
			return Item{}, err
		}
		return rec.Items[i], nil
	}
	return Item{}, fmt.Errorf("item not found")
}

func (s *Store) RecordExecution(id, jobID string) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadLocked()
	if err != nil {
		return Item{}, err
	}
	id = strings.TrimSpace(id)
	jobID = strings.TrimSpace(jobID)
	if id == "" || jobID == "" {
		return Item{}, fmt.Errorf("id and execution_job_id required")
	}
	for i := range rec.Items {
		if rec.Items[i].ID != id {
			continue
		}
		if rec.Items[i].Status != StatusOpen {
			return Item{}, fmt.Errorf("item not open")
		}
		rec.Items[i].ExecutionJobID = jobID
		rec.Items[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := s.saveLocked(rec); err != nil {
			return Item{}, err
		}
		return rec.Items[i], nil
	}
	return Item{}, fmt.Errorf("item not found")
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

// AddChecklistDispatch adds a checklist_dispatch handoff unless one for the same
// checklist item is already open; the bool reports whether it created anything.
//
// The check runs under the store lock, like the pending_id guard in Add: the
// husbandry sync fires several times within a second or two (Console refresh,
// sweep), and a check made outside the lock let every one of them through.
func (s *Store) AddChecklistDispatch(item Item, checklistItemID string) (Item, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, err := s.loadLocked()
	if err != nil {
		return Item{}, false, err
	}
	if want := strings.TrimSpace(checklistItemID); want != "" {
		for _, existing := range rec.Items {
			if existing.Status != StatusOpen || existing.Source != SourceChecklistDispatch {
				continue
			}
			if ExtractChecklistItemID(existing) == want {
				return existing, false, nil
			}
		}
	}
	rec.Items = append(rec.Items, item)
	if err := s.saveLocked(rec); err != nil {
		return Item{}, false, err
	}
	return item, true, nil
}

func NewItemFromApproval(params ApprovalInjectParams) Item {
	now := time.Now().UTC().Format(time.RFC3339)
	lane := strings.TrimSpace(params.OperateLane)
	if lane != "" && !ValidLanes[lane] {
		lane = ""
	}
	return Item{
		ID: uuid.New().String(), ProgramID: params.ProgramID,
		SourceLaneID: params.SourceLaneID, Lane: lane, OperateLane: lane,
		Title: params.Title, Description: params.Description,
		HandoffKind: defaultString(params.HandoffKind, HandoffOneOff),
		Reason:      params.Reason, AgentTaskID: params.AgentTaskID,
		AcceptanceCriteria: cleanStrings(params.AcceptanceCriteria),
		VerificationSteps:  cleanStrings(params.VerificationSteps),
		RiskLevel:          defaultString(params.RiskLevel, RiskLow),
		Owner:              params.Owner, DueAt: params.DueAt,
		Status: StatusOpen, CreatedAt: now, UpdatedAt: now,
		Source: SourcePostCompletion, PendingID: params.PendingID, ApprovedBy: params.ApprovedBy,
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
	lane := strings.TrimSpace(req.OperateLane)
	if lane == "" {
		lane = strings.TrimSpace(req.Lane)
	}
	if lane != "" && !ValidLanes[lane] {
		return Item{}, fmt.Errorf("invalid lane")
	}
	if err := validateOptionalStructuredFields(req.HandoffKind, req.RiskLevel, req.AgentTaskID, req.DueAt); err != nil {
		return Item{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	return Item{
		ID: uuid.New().String(), ProgramID: programID, SourceLaneID: strings.TrimSpace(req.SourceLaneID),
		Lane: lane, OperateLane: lane, Title: title, Description: strings.TrimSpace(req.Description),
		HandoffKind: defaultString(req.HandoffKind, HandoffOneOff), Reason: strings.TrimSpace(req.Reason),
		AgentTaskID:        strings.TrimSpace(req.AgentTaskID),
		AcceptanceCriteria: cleanStrings(req.AcceptanceCriteria), VerificationSteps: cleanStrings(req.VerificationSteps),
		RiskLevel: defaultString(req.RiskLevel, RiskLow), Owner: strings.TrimSpace(req.Owner),
		DueAt: strings.TrimSpace(req.DueAt), Status: StatusOpen, CreatedAt: now, UpdatedAt: now,
		Source: SourceManual,
	}, nil
}

func ValidateStructuredHandoff(item EnqueueRequest) error {
	if strings.TrimSpace(item.Reason) == "" {
		return fmt.Errorf("reason required")
	}
	if strings.TrimSpace(item.OperateLane) == "" || !ValidLanes[strings.TrimSpace(item.OperateLane)] {
		return fmt.Errorf("valid operate_lane required")
	}
	if len(cleanStrings(item.AcceptanceCriteria)) == 0 {
		return fmt.Errorf("acceptance_criteria required")
	}
	if len(cleanStrings(item.VerificationSteps)) == 0 {
		return fmt.Errorf("verification_steps required")
	}
	if strings.TrimSpace(item.RiskLevel) == "" {
		return fmt.Errorf("risk_level required")
	}
	return validateOptionalStructuredFields(item.HandoffKind, item.RiskLevel, item.AgentTaskID, item.DueAt)
}

func validateOptionalStructuredFields(kind, risk, agentTaskID, dueAt string) error {
	if kind != "" && kind != HandoffOneOff && kind != HandoffRecurringSetup {
		return fmt.Errorf("invalid handoff_kind")
	}
	if risk != "" && risk != RiskLow && risk != RiskMedium && risk != RiskHigh {
		return fmt.Errorf("invalid risk_level")
	}
	if agentTaskID != "" {
		if _, ok := agentgovernance.TaskByID(agentTaskID); !ok {
			return fmt.Errorf("invalid agent_task_id")
		}
	}
	if dueAt != "" {
		if _, err := time.Parse(time.RFC3339, dueAt); err != nil {
			return fmt.Errorf("due_at must be RFC3339")
		}
	}
	return nil
}

func normalizeLegacyItem(item *Item) {
	if item.OperateLane == "" {
		item.OperateLane = item.Lane
	}
	if item.Lane == "" {
		item.Lane = item.OperateLane
	}
	if item.HandoffKind == "" {
		item.HandoffKind = HandoffOneOff
	}
	if item.RiskLevel == "" {
		item.RiskLevel = RiskLow
	}
}

func cleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func hasRecurringEvidence(values []string) bool {
	for _, value := range values {
		lower := strings.ToLower(value)
		if strings.HasPrefix(lower, "schedule:") || strings.HasPrefix(lower, "skill:") || strings.HasPrefix(lower, "operator:") {
			return true
		}
	}
	return false
}

func hasEvidence(values []string, expected string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), expected) {
			return true
		}
	}
	return false
}
