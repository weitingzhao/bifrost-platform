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

const (
	briefsStateVersion = "2026-07-19"
	holdDuration       = 72 * time.Hour
)

type BriefStore struct {
	path string
	mu   sync.Mutex
}

func NewBriefStore(configDir string) *BriefStore {
	dataDir := os.Getenv("PLATFORM_DATA_DIR")
	if dataDir == "" {
		dataDir = filepath.Join(configDir, "..", "data")
	}
	dir := filepath.Join(dataDir, "operate")
	return &BriefStore{path: filepath.Join(dir, "decision-briefs.json")}
}

func (s *BriefStore) Path() string { return s.path }

func (s *BriefStore) loadLocked() (*BriefsFileRecord, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return &BriefsFileRecord{Version: briefsStateVersion, Briefs: []DecisionBrief{}}, nil
		}
		return nil, fmt.Errorf("read decision briefs: %w", err)
	}
	var rec BriefsFileRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("parse decision briefs: %w", err)
	}
	if rec.Briefs == nil {
		rec.Briefs = []DecisionBrief{}
	}
	if rec.Version == "" {
		rec.Version = briefsStateVersion
	}
	return &rec, nil
}

func (s *BriefStore) saveLocked(rec *BriefsFileRecord) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir decision briefs: %w", err)
	}
	rec.Version = briefsStateVersion
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write decision briefs: %w", err)
	}
	return os.Rename(tmp, s.path)
}

// ListPending returns undecided briefs (and active holds that have not expired).
func (s *BriefStore) ListPending(now time.Time) ([]DecisionBrief, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	out := make([]DecisionBrief, 0)
	for _, b := range rec.Briefs {
		if strings.TrimSpace(b.Decision) == "" {
			out = append(out, b)
			continue
		}
		if b.Decision == DecisionHold {
			if until, err := time.Parse(time.RFC3339, b.HoldUntil); err == nil && now.Before(until) {
				out = append(out, b)
			}
		}
	}
	return out, nil
}

// HoldUntilForItem returns active hold expiry for an item, if any.
func (s *BriefStore) HoldUntilForItem(itemID string, now time.Time) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadLocked()
	if err != nil {
		return ""
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	itemID = strings.TrimSpace(itemID)
	for i := len(rec.Briefs) - 1; i >= 0; i-- {
		b := rec.Briefs[i]
		if b.ItemID != itemID || b.Decision != DecisionHold {
			continue
		}
		until, err := time.Parse(time.RFC3339, b.HoldUntil)
		if err != nil || !now.Before(until) {
			continue
		}
		return b.HoldUntil
	}
	return ""
}

// UpsertPending creates or refreshes an undecided brief for the item.
func (s *BriefStore) UpsertPending(brief DecisionBrief) (DecisionBrief, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadLocked()
	if err != nil {
		return DecisionBrief{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range rec.Briefs {
		b := &rec.Briefs[i]
		if b.ItemID == brief.ItemID && strings.TrimSpace(b.Decision) == "" {
			brief.ID = b.ID
			if brief.CreatedAt == "" {
				brief.CreatedAt = b.CreatedAt
			}
			rec.Briefs[i] = brief
			if err := s.saveLocked(rec); err != nil {
				return DecisionBrief{}, err
			}
			return brief, nil
		}
	}
	if brief.ID == "" {
		brief.ID = uuid.New().String()
	}
	if brief.CreatedAt == "" {
		brief.CreatedAt = now
	}
	rec.Briefs = append(rec.Briefs, brief)
	if err := s.saveLocked(rec); err != nil {
		return DecisionBrief{}, err
	}
	return brief, nil
}

func (s *BriefStore) FindByID(id string) (*DecisionBrief, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadLocked()
	if err != nil {
		return nil, false
	}
	for i := range rec.Briefs {
		if rec.Briefs[i].ID == id {
			b := rec.Briefs[i]
			return &b, true
		}
	}
	return nil, false
}

func (s *BriefStore) ApplyDecision(id, decision string) (DecisionBrief, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadLocked()
	if err != nil {
		return DecisionBrief{}, err
	}
	id = strings.TrimSpace(id)
	decision = strings.TrimSpace(decision)
	switch decision {
	case DecisionApprovedRun, DecisionDismissed, DecisionHold:
	default:
		return DecisionBrief{}, fmt.Errorf("decision must be approved_run, dismissed, or hold")
	}
	now := time.Now().UTC()
	for i := range rec.Briefs {
		if rec.Briefs[i].ID != id {
			continue
		}
		if strings.TrimSpace(rec.Briefs[i].Decision) != "" && rec.Briefs[i].Decision != DecisionHold {
			return DecisionBrief{}, fmt.Errorf("brief already decided")
		}
		rec.Briefs[i].Decision = decision
		rec.Briefs[i].DecidedAt = now.Format(time.RFC3339)
		if decision == DecisionHold {
			rec.Briefs[i].HoldUntil = now.Add(holdDuration).Format(time.RFC3339)
		} else {
			rec.Briefs[i].HoldUntil = ""
		}
		if err := s.saveLocked(rec); err != nil {
			return DecisionBrief{}, err
		}
		return rec.Briefs[i], nil
	}
	return DecisionBrief{}, fmt.Errorf("brief not found")
}

// BuildDecisionBrief constructs a markdown-backed brief from triage evidence.
func BuildDecisionBrief(item Item, classified ClassifyResult, now time.Time) DecisionBrief {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	suggestion, suggestionReason, openQ := suggestFromClassify(classified)
	age := formatItemAge(item.CreatedAt, now)
	scope := classified.FixScope
	if scope == "" {
		scope = "none"
	}
	brief := DecisionBrief{
		ItemID:           item.ID,
		Title:            item.Title,
		CreatedAt:        now.Format(time.RFC3339),
		FleetSignal:      classified.FleetSignal,
		FleetDetail:      classified.FleetDetail,
		ItemAge:          age,
		FixScope:         classified.FixScope,
		RiskLevel:        defaultString(item.RiskLevel, RiskLow),
		Source:           item.Source,
		Suggestion:       suggestion,
		SuggestionReason: suggestionReason,
		OpenQuestion:     openQ,
		FailingStandards: classified.FailingStandards,
	}
	brief.FullBrief = renderFullBrief(brief, item, scope)
	return brief
}

func suggestFromClassify(c ClassifyResult) (suggestion, reason, openQ string) {
	switch {
	case strings.Contains(c.Reason, "D10"):
		return SuggestionHold, c.Reason, "Confirm this is observe-only and decide whether to dismiss or keep for manual IB triage."
	case c.Reason == "handoff_kind=recurring_setup requires owner verification":
		return SuggestionHold, c.Reason, "Has the recurring schedule/skill/operator setup been verified?"
	case strings.EqualFold(c.FleetSignal, "GO"):
		return SuggestionDismiss, "Fleet evidence looks green — dismiss if handoff is obsolete.", "Is this handoff still actionable?"
	case strings.EqualFold(c.FleetSignal, "NO-GO") && c.FixScope != "":
		return SuggestionRUN, "Scope still NO-GO with a valid fix path — approve RUN to drain.", "Any reason not to start remediation for this scope?"
	default:
		return SuggestionHold, c.Reason, "Need owner judgment before auto-drain."
	}
}

func renderFullBrief(b DecisionBrief, item Item, scopeDisplay string) string {
	failing := b.FailingStandards
	if failing == "" {
		failing = "none"
	}
	openQ := b.OpenQuestion
	if openQ == "" {
		openQ = "(none)"
	}
	return fmt.Sprintf(`## Decision Brief: %s

**Item**: %s | **Age**: %s | **Risk**: %s
**Source**: %s | **Scope**: %s

### Fleet Evidence
- Cell: %s → %s
- Standards: %s
- Detail: %s

### Suggestion: %s
%s

### Open Question
%s

---
Copy this brief to Cursor IDE Agent for analysis, or decide below.
`,
		b.Title,
		item.ID, b.ItemAge, b.RiskLevel,
		defaultString(item.Source, "unknown"), scopeDisplay,
		b.FleetDetail, b.FleetSignal,
		failing,
		b.FleetDetail,
		b.Suggestion,
		b.SuggestionReason,
		openQ,
	)
}
