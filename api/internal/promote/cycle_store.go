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

const maxCycleEntries = 50

// CycleStore persists release cycle history as JSON files under the platform data dir.
type CycleStore struct {
	baseDir string
	mu      sync.RWMutex
}

// NewCycleStore creates a CycleStore rooted at the same data directory as gate state.
func NewCycleStore(configDir string) *CycleStore {
	dataDir := os.Getenv("PLATFORM_DATA_DIR")
	if dataDir == "" {
		dataDir = filepath.Join(configDir, "..", "data")
	}
	if override := strings.TrimSpace(os.Getenv("PLATFORM_RELEASE_CYCLES_DIR")); override != "" {
		dataDir = override
	}
	return &CycleStore{baseDir: dataDir}
}

func (s *CycleStore) path(lane ReleaseCycleLane) string {
	switch lane {
	case ReleaseCycleLanePlatform:
		return filepath.Join(s.baseDir, "release_cycles_platform.json")
	default:
		return filepath.Join(s.baseDir, "release_cycles_trade.json")
	}
}

func (s *CycleStore) loadLocked(lane ReleaseCycleLane) ([]ReleaseCycleRecord, error) {
	path := s.path(lane)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read release cycles: %w", err)
	}
	var entries []ReleaseCycleRecord
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse release cycles: %w", err)
	}
	return entries, nil
}

func (s *CycleStore) saveLocked(lane ReleaseCycleLane, entries []ReleaseCycleRecord) error {
	if len(entries) > maxCycleEntries {
		entries = entries[len(entries)-maxCycleEntries:]
	}
	path := s.path(lane)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir release cycles dir: %w", err)
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write release cycles: %w", err)
	}
	return os.Rename(tmp, path)
}

// List returns cycles newest-first (max 50).
func (s *CycleStore) List(lane ReleaseCycleLane) ([]ReleaseCycleRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, err := s.loadLocked(lane)
	if err != nil {
		return nil, err
	}
	if len(entries) > maxCycleEntries {
		entries = entries[len(entries)-maxCycleEntries:]
	}
	out := make([]ReleaseCycleRecord, len(entries))
	copy(out, entries)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// Get returns a cycle by id across both lanes.
func (s *CycleStore) Get(id string) (*ReleaseCycleRecord, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, lane := range []ReleaseCycleLane{ReleaseCycleLaneTrade, ReleaseCycleLanePlatform} {
		entries, err := s.loadLocked(lane)
		if err != nil {
			return nil, err
		}
		for i := range entries {
			if entries[i].ID == id {
				rec := entries[i]
				return &rec, nil
			}
		}
	}
	return nil, nil
}

// ActiveCycle returns the newest in_progress cycle for the lane, if any.
func (s *CycleStore) ActiveCycle(lane ReleaseCycleLane) (*ReleaseCycleRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, err := s.loadLocked(lane)
	if err != nil {
		return nil, err
	}
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Outcome == CycleOutcomeInProgress {
			rec := entries[i]
			return &rec, nil
		}
	}
	return nil, nil
}

type DeployRecordOpts struct {
	Lane           ReleaseCycleLane
	Step           CycleStepKind
	Revision       string
	RunName        string
	TriggeredBy    string
	AgentSessionID string
}

// RecordDeploy opens or updates a cycle when a deliver PipelineRun is started.
func (s *CycleStore) RecordDeploy(opts DeployRecordOpts) (*ReleaseCycleRecord, error) {
	lane := opts.Lane
	if lane == "" {
		return nil, fmt.Errorf("lane required")
	}
	rev := strings.TrimSpace(opts.Revision)
	if rev == "" {
		rev = "main"
	}
	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.loadLocked(lane)
	if err != nil {
		return nil, err
	}

	activeIdx := -1
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Outcome == CycleOutcomeInProgress {
			activeIdx = i
			break
		}
	}

	openNew := activeIdx < 0
	if activeIdx >= 0 {
		active := &entries[activeIdx]
		// New STG deploy with a different revision supersedes the prior cycle.
		if opts.Step == CycleStepStgDeploy && strings.TrimSpace(active.Revision) != "" && active.Revision != rev {
			active.Outcome = CycleOutcomeSuperseded
			t := now
			active.CompletedAt = &t
			openNew = true
		}
	}

	// Reopen a failed cycle for the same revision on retry (do not create a duplicate).
	if openNew {
		for i := len(entries) - 1; i >= 0; i-- {
			if entries[i].Outcome != CycleOutcomeFailed {
				continue
			}
			if entries[i].Revision != rev {
				continue
			}
			entries[i].Outcome = CycleOutcomeInProgress
			entries[i].CompletedAt = nil
			activeIdx = i
			openNew = false
			break
		}
	}

	if openNew {
		rec := ReleaseCycleRecord{
			ID:             newCycleID(now),
			Lane:           lane,
			Revision:       rev,
			Outcome:        CycleOutcomeInProgress,
			StartedAt:      now,
			Steps:          emptyCycleSteps(),
			TriggeredBy:    opts.TriggeredBy,
			AgentSessionID: opts.AgentSessionID,
		}
		setStepDeploy(&rec, opts.Step, opts.RunName, now)
		entries = append(entries, rec)
		if err := s.saveLocked(lane, entries); err != nil {
			return nil, err
		}
		return &rec, nil
	}

	active := &entries[activeIdx]
	if active.Revision == "" {
		active.Revision = rev
	}
	if active.TriggeredBy == "" && opts.TriggeredBy != "" {
		active.TriggeredBy = opts.TriggeredBy
	}
	if active.AgentSessionID == "" && opts.AgentSessionID != "" {
		active.AgentSessionID = opts.AgentSessionID
	}
	setStepDeploy(active, opts.Step, opts.RunName, now)
	if err := s.saveLocked(lane, entries); err != nil {
		return nil, err
	}
	out := *active
	return &out, nil
}

func setStepDeploy(rec *ReleaseCycleRecord, kind CycleStepKind, runName string, now time.Time) {
	for i := range rec.Steps {
		if rec.Steps[i].Kind != kind {
			continue
		}
		t := now
		if rec.Steps[i].StartedAt == nil {
			rec.Steps[i].StartedAt = &t
		} else {
			// Retry: refresh start time for the new run.
			rec.Steps[i].StartedAt = &t
		}
		rec.Steps[i].CompletedAt = nil
		rec.Steps[i].Result = CycleStepResultRunning
		rec.Steps[i].RunName = runName
		rec.Steps[i].Detail = fmt.Sprintf("PipelineRun %s started", runName)
		return
	}
}

type GateRecordOpts struct {
	Lane        ReleaseCycleLane
	Step        CycleStepKind
	Revision    string
	Result      string // pass | fail
	Checks      []GateCheck
	TriggeredBy string
	Summary     string
}

// RecordGate updates the active cycle with a gate result.
// Prod gate pass completes the cycle as released. Failures update the step but keep the cycle open for retry.
func (s *CycleStore) RecordGate(opts GateRecordOpts) (*ReleaseCycleRecord, error) {
	lane := opts.Lane
	if lane == "" {
		return nil, fmt.Errorf("lane required")
	}
	now := time.Now().UTC()
	rev := strings.TrimSpace(opts.Revision)

	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.loadLocked(lane)
	if err != nil {
		return nil, err
	}

	activeIdx := -1
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Outcome == CycleOutcomeInProgress {
			activeIdx = i
			break
		}
	}

	if activeIdx < 0 {
		// Gate run without a prior deploy start — open a cycle so history is not lost.
		rec := ReleaseCycleRecord{
			ID:          newCycleID(now),
			Lane:        lane,
			Revision:    rev,
			Outcome:     CycleOutcomeInProgress,
			StartedAt:   now,
			Steps:       emptyCycleSteps(),
			TriggeredBy: opts.TriggeredBy,
		}
		applyGateStep(&rec, opts, now)
		finalizeGateOutcome(&rec, opts, now)
		entries = append(entries, rec)
		if err := s.saveLocked(lane, entries); err != nil {
			return nil, err
		}
		return &rec, nil
	}

	active := &entries[activeIdx]
	if active.Revision == "" && rev != "" {
		active.Revision = rev
	}
	if active.TriggeredBy == "" && opts.TriggeredBy != "" {
		active.TriggeredBy = opts.TriggeredBy
	}
	applyGateStep(active, opts, now)
	finalizeGateOutcome(active, opts, now)
	if err := s.saveLocked(lane, entries); err != nil {
		return nil, err
	}
	out := *active
	return &out, nil
}

func applyGateStep(rec *ReleaseCycleRecord, opts GateRecordOpts, now time.Time) {
	// Mark the preceding deploy step succeeded when gate runs (deploy must have completed for gate to be meaningful).
	deployKind := CycleStepStgDeploy
	if opts.Step == CycleStepProdGate {
		deployKind = CycleStepProdDeploy
	}
	for i := range rec.Steps {
		if rec.Steps[i].Kind != deployKind {
			continue
		}
		if rec.Steps[i].Result == CycleStepResultRunning || rec.Steps[i].Result == "" {
			t := now
			if rec.Steps[i].StartedAt == nil {
				rec.Steps[i].StartedAt = &t
			}
			rec.Steps[i].CompletedAt = &t
			rec.Steps[i].Result = CycleStepResultSuccess
			if rec.Steps[i].Detail == "" {
				rec.Steps[i].Detail = "Deploy accepted by gate"
			}
		}
		break
	}

	for i := range rec.Steps {
		if rec.Steps[i].Kind != opts.Step {
			continue
		}
		t := now
		if rec.Steps[i].StartedAt == nil {
			rec.Steps[i].StartedAt = &t
		}
		rec.Steps[i].CompletedAt = &t
		if opts.Result == "pass" {
			rec.Steps[i].Result = CycleStepResultPass
		} else {
			rec.Steps[i].Result = CycleStepResultFail
		}
		rec.Steps[i].Detail = opts.Summary
		rec.Steps[i].GateChecks = opts.Checks
		return
	}
}

func finalizeGateOutcome(rec *ReleaseCycleRecord, opts GateRecordOpts, now time.Time) {
	if opts.Step == CycleStepProdGate && opts.Result == "pass" {
		rec.Outcome = CycleOutcomeReleased
		t := now
		rec.CompletedAt = &t
		return
	}
	// Keep in_progress on fail so retries can update the same cycle.
	if opts.Result == "fail" {
		// Surface failure on the cycle without closing it permanently —
		// leave outcome as in_progress; UI can show the failed step.
		return
	}
}

func newCycleID(now time.Time) string {
	return fmt.Sprintf("rc-%d", now.UnixNano())
}

// RunStatusInfo is a PipelineRun status snapshot used to sync cycle deploy steps.
type RunStatusInfo struct {
	RunName string
	Status  string // Tekton condition status: True | False | Unknown
	Reason  string // Succeeded | Failed | Running | …
}

// SyncRunStatus updates running deploy steps from K8s PipelineRun terminal status.
// Deploy Failed marks the cycle outcome as failed; Succeeded marks the step success.
func (s *CycleStore) SyncRunStatus(lane ReleaseCycleLane, statuses []RunStatusInfo) error {
	if len(statuses) == 0 {
		return nil
	}
	byName := make(map[string]RunStatusInfo, len(statuses))
	for _, st := range statuses {
		name := strings.TrimSpace(st.RunName)
		if name == "" {
			continue
		}
		byName[name] = st
	}
	if len(byName) == 0 {
		return nil
	}

	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.loadLocked(lane)
	if err != nil {
		return err
	}

	changed := false
	for i := range entries {
		if entries[i].Outcome != CycleOutcomeInProgress {
			continue
		}
		for j := range entries[i].Steps {
			step := &entries[i].Steps[j]
			if step.Result != CycleStepResultRunning || step.RunName == "" {
				continue
			}
			st, ok := byName[step.RunName]
			if !ok {
				continue
			}
			term := classifyRunTerminal(st.Status, st.Reason)
			if term == "" {
				continue
			}
			t := now
			step.CompletedAt = &t
			if term == CycleStepResultSuccess {
				step.Result = CycleStepResultSuccess
				step.Detail = fmt.Sprintf("PipelineRun %s succeeded", step.RunName)
			} else {
				step.Result = CycleStepResultFailed
				detail := fmt.Sprintf("PipelineRun %s failed", step.RunName)
				if st.Reason != "" {
					detail += " (" + st.Reason + ")"
				}
				step.Detail = detail
				// Deploy failure closes the cycle; retry via RecordDeploy reopens it.
				if step.Kind == CycleStepStgDeploy || step.Kind == CycleStepProdDeploy {
					entries[i].Outcome = CycleOutcomeFailed
					entries[i].CompletedAt = &t
				}
			}
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.saveLocked(lane, entries)
}

// classifyRunTerminal returns success/failed for terminal runs, or "" if still running.
func classifyRunTerminal(status, reason string) string {
	st := strings.ToLower(strings.TrimSpace(status))
	re := strings.ToLower(strings.TrimSpace(reason))
	if st == "true" || st == "succeeded" || re == "succeeded" || re == "completed" {
		return CycleStepResultSuccess
	}
	if st == "false" || st == "failed" || re == "failed" || re == "cancelled" || re == "canceled" {
		return CycleStepResultFailed
	}
	return ""
}

// RunningDeploySteps returns run names still marked running on the active cycle.
func (s *CycleStore) RunningDeploySteps(lane ReleaseCycleLane) ([]CycleStepRecord, error) {
	active, err := s.ActiveCycle(lane)
	if err != nil || active == nil {
		return nil, err
	}
	out := make([]CycleStepRecord, 0, 2)
	for _, step := range active.Steps {
		if step.Result == CycleStepResultRunning && step.RunName != "" &&
			(step.Kind == CycleStepStgDeploy || step.Kind == CycleStepProdDeploy) {
			out = append(out, step)
		}
	}
	return out, nil
}

// PipelineForCycleStep maps a lane+step to the Tekton deliver pipeline name.
func PipelineForCycleStep(lane ReleaseCycleLane, kind CycleStepKind) string {
	switch lane {
	case ReleaseCycleLanePlatform:
		switch kind {
		case CycleStepStgDeploy:
			return "bifrost-deliver-platform"
		case CycleStepProdDeploy:
			return "bifrost-deliver-platform-prod"
		}
	default:
		switch kind {
		case CycleStepStgDeploy:
			return "bifrost-deliver-stg"
		case CycleStepProdDeploy:
			return "bifrost-deliver-prod"
		}
	}
	return ""
}

// RecordDeployFromPipeline is a convenience for delivery hooks.
func (s *CycleStore) RecordDeployFromPipeline(pipelineName, revision, runName, triggeredBy, agentSessionID string) (*ReleaseCycleRecord, error) {
	lane, step, ok := LaneForPipeline(pipelineName)
	if !ok {
		return nil, nil
	}
	return s.RecordDeploy(DeployRecordOpts{
		Lane:           lane,
		Step:           step,
		Revision:       revision,
		RunName:        runName,
		TriggeredBy:    triggeredBy,
		AgentSessionID: agentSessionID,
	})
}

// RecordGateFromTier is a convenience for promote.RunReleaseGate.
func (s *CycleStore) RecordGateFromTier(tier GateTier, revision, result, triggeredBy, summary string, checks []GateCheck) (*ReleaseCycleRecord, error) {
	step, ok := GateStepForTier(tier)
	if !ok {
		return nil, nil
	}
	return s.RecordGate(GateRecordOpts{
		Lane:        LaneForGateTier(tier),
		Step:        step,
		Revision:    revision,
		Result:      result,
		Checks:      checks,
		TriggeredBy: triggeredBy,
		Summary:     summary,
	})
}
