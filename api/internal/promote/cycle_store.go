package promote

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
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
	data, err := statefile.ReadFile(path)
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
	if err := statefile.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write release cycles: %w", err)
	}
	return nil
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
