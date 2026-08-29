package devagent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/operatequeue"
)

type PhaseStatus string

const (
	PhasePending PhaseStatus = "pending"
	PhaseRunning PhaseStatus = "running"
	PhaseDone    PhaseStatus = "done"
	PhaseFailed  PhaseStatus = "failed"
)

type JobStatus string

const (
	JobIdle           JobStatus = "idle"
	JobRunning        JobStatus = "running"
	JobAwaitingReview JobStatus = "awaiting_review"
	JobDone           JobStatus = "done"
	JobFailed         JobStatus = "failed"
	JobCancelled      JobStatus = "cancelled"
)

type Phase struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Status      PhaseStatus `json:"status"`
	StartedAt   string      `json:"started_at,omitempty"`
	CompletedAt string      `json:"completed_at,omitempty"`
}

type Job struct {
	ID            string    `json:"id"`
	PhaseID       string    `json:"phase_id"`
	Status        JobStatus `json:"status"`
	Output        string    `json:"output"`
	Summary       string    `json:"summary,omitempty"`
	StartedAt     string    `json:"started_at,omitempty"`
	CompletedAt   string    `json:"completed_at,omitempty"`
	ApprovedBy    string    `json:"approved_by,omitempty"`
	CursorAgentID string    `json:"cursor_agent_id,omitempty"`
}

type ProgramJobsResponse struct {
	ProgramID string `json:"program_id"`
	ActiveJob *Job   `json:"active_job"`
	History   []Job  `json:"history"`
}

type BridgeConfig struct {
	Workspace   string `json:"workspace"`
	Model       string `json:"model"`
	SkillPath   string `json:"skill_path,omitempty"`
	SkillLoaded bool   `json:"skill_loaded"`
}

type programRuntime struct {
	blueprint *ProgramBlueprint
	phases    []Phase
	activeJob *Job
	history   []Job
	state     *ProgramStateRecord
}

type Handler struct {
	mu             sync.Mutex
	runtimes       map[string]*programRuntime
	blueprintDir   string
	repoRoot       string
	configDir      string
	store          *FileStore
	operateQueue   *operatequeue.Handler
	sessionStore   sessionValidator
	audit          *actuation.AuditLog
	namingWarnings []NamingWarning
}

// sessionValidator is the progress-hook surface from api/internal/sessions.
type sessionValidator interface {
	ValidateProgressHook(sessionID, programID, phaseID string) error
}

func (h *Handler) BindOperateQueue(oq *operatequeue.Handler) {
	h.operateQueue = oq
}

func (h *Handler) BindAudit(a *actuation.AuditLog) {
	h.audit = a
}

func (h *Handler) BindSessions(sv sessionValidator) {
	h.sessionStore = sv
}

func NewHandler(configDir string) (*Handler, error) {
	programsDir := filepath.Join(configDir, "programs")
	if err := InitProgramTemplates(configDir); err != nil {
		// Temp-dir unit tests may omit _templates.yaml; fall back to repo discovery.
		if err2 := ensureTemplatesLoaded(); err2 != nil {
			return nil, fmt.Errorf("program templates: %w", err)
		}
	}
	blueprints, err := LoadProgramBlueprints(programsDir)
	if err != nil {
		return nil, err
	}
	for _, bp := range blueprints {
		for _, warning := range validateGateRules(bp) {
			slog.Warn("program gate validation", "program_id", bp.ID, "warning", warning)
		}
	}

	store := NewFileStore(configDir)
	repoRoot := filepath.Dir(configDir)
	h := &Handler{
		repoRoot:       repoRoot,
		configDir:      configDir,
		blueprintDir:   programsDir,
		runtimes:       make(map[string]*programRuntime, len(blueprints)),
		store:          store,
		namingWarnings: CollectNamingWarnings(blueprints),
	}

	for _, bp := range blueprints {
		rt := &programRuntime{
			blueprint: bp,
			phases:    phasesFromBlueprint(bp),
			history:   []Job{},
		}
		if saved, loadErr := store.LoadProgram(bp.ID); loadErr != nil {
			return nil, loadErr
		} else if saved != nil {
			remapped := RemapPhaseIDs(saved, phaseIDAliasMap(bp))
			rt.phases = mergePhasesFromState(bp, saved.Phases)
			rt.activeJob = saved.ActiveJob
			rt.history = saved.History
			rt.state = saved
			if rt.history == nil {
				rt.history = []Job{}
			}
			if remapped {
				h.runtimes[bp.ID] = rt
				if persistErr := h.persistRuntimeLocked(bp.ID); persistErr != nil {
					return nil, persistErr
				}
			}
		} else {
			rt.state = &ProgramStateRecord{ProgramID: bp.ID, History: []Job{}}
		}
		h.runtimes[bp.ID] = rt
	}

	if err := h.syncVisionSignoffsFromGateFiles(); err != nil {
		return nil, err
	}

	h.blueprintDir = programsDir
	h.logLiveLaneCollisions()
	return h, nil
}

func (h *Handler) logLiveLaneCollisions() {
	h.mu.Lock()
	collisions := h.liveLaneCollisionsLocked()
	h.mu.Unlock()
	for _, c := range collisions {
		slog.Error("D2 live lane collision at startup",
			"lane_id", c.LaneID,
			"programs", strings.Join(c.ProgramIDs, ","),
		)
	}
}

func (h *Handler) persistRuntimeLocked(programID string) error {
	rt, ok := h.runtimes[programID]
	if !ok {
		return fmt.Errorf("program runtime not found: %s", programID)
	}
	// Closed catalog status freezes routine phase/job writes, but Owner post-completion
	// assessment (NO HANDOFF / closed) must still land on disk or Briefing loses it on restart.
	if rt.blueprint != nil && isClosedProgramStatus(rt.blueprint.Status) {
		if rt.state == nil || rt.state.PostCompletion == nil ||
			strings.TrimSpace(rt.state.PostCompletion.AssessmentStatus) == "" {
			return nil
		}
	}
	if rt.state == nil {
		rt.state = &ProgramStateRecord{ProgramID: programID, History: []Job{}}
	}
	rt.state.ProgramID = programID
	rt.state.Phases = rt.phases
	rt.state.ActiveJob = rt.activeJob
	rt.state.History = rt.history
	return h.store.SaveProgramRecord(rt.state)
}

func (h *Handler) HandlePrograms(w http.ResponseWriter, r *http.Request) {
	boardOnly := r.URL.Query().Get("board") == "1" || r.URL.Query().Get("board") == "true"
	templateFilter := strings.TrimSpace(r.URL.Query().Get("template_id"))
	laneFilter := strings.TrimSpace(r.URL.Query().Get("lane_id"))
	statusFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	includeArchived := queryFlagTrue(r.URL.Query().Get("include_archived"))
	h.mu.Lock()
	defer h.mu.Unlock()

	list := make([]ProgramSummary, 0, len(h.runtimes))
	for id, rt := range h.runtimes {
		bpStatus := ""
		if rt.blueprint != nil {
			bpStatus = strings.ToLower(strings.TrimSpace(rt.blueprint.Status))
		}
		if statusFilter != "" {
			if bpStatus != statusFilter {
				continue
			}
		} else if !includeArchived && isArchivedStatus(bpStatus) {
			continue
		}
		if boardOnly && (rt.blueprint.Delivery == nil || !rt.blueprint.Delivery.BoardVisible) {
			continue
		}
		if templateFilter != "" && templateIDFromRuntime(rt) != templateFilter {
			continue
		}
		sum := h.buildProgramSummary(id, rt)
		if boardOnly {
			sum.Phases = h.compactPhasesForBoard(rt)
		}
		if laneFilter != "" && sum.LaneID != laneFilter {
			continue
		}
		list = append(list, sum)
	}
	warnings := h.namingWarnings
	if warnings == nil {
		warnings = []NamingWarning{}
	}
	// Collisions are a full-runtime scan (same as startup). Board/lane filters
	// must not hide a D2 break that still exists in memory.
	writeJSON(w, http.StatusOK, map[string]any{
		"programs":             list,
		"live_lane_collisions": h.liveLaneCollisionsLocked(),
		"naming_warnings":      warnings,
	})
}

func (h *Handler) buildProgramSummary(programID string, rt *programRuntime) ProgramSummary {
	doneFromPhases := make(map[string]bool, len(rt.phases))
	for _, p := range rt.phases {
		if p.Status == PhaseDone {
			doneFromPhases[p.ID] = true
		}
	}
	if rt.state != nil {
		for _, pr := range rt.state.PhaseProgress {
			if pr.Status == "done" {
				doneFromPhases[pr.PhaseID] = true
			}
		}
	}
	phasesDone := len(doneFromPhases)
	signed := h.countSignedPhases(rt)
	phaseCount := len(rt.blueprint.Phases)
	gatesRequired := countSignOffRequiredPhases(rt.blueprint)
	complete := programCompleteFromGates(gatesRequired, signed, phaseCount, phasesDone)
	allPhasesDone := phaseCount > 0 && phasesDone == phaseCount
	pendingCount, promptReady := pendingAndPromptReady(rt.blueprint, doneFromPhases)
	jobStatus := runtimeJobStatusFromJob(rt.activeJob)
	summary := ProgramSummary{
		ID:                   programID,
		Title:                rt.blueprint.Title,
		Label:                rt.blueprint.Title,
		Description:          rt.blueprint.Description,
		Status:               rt.blueprint.Status,
		PhaseCount:           phaseCount,
		PhasesDone:           phasesDone,
		PhasesSigned:         signed,
		Signed:               signed,
		SignOffRequiredCount: gatesRequired,
		Complete:             complete,
		AllPhasesDone:        allPhasesDone,
		Active:               isSelectableActiveStatus(rt.blueprint.Status),
		Delivery:             rt.blueprint.Delivery,
		RuntimeJobStatus:     jobStatus,
		PendingCount:         pendingCount,
		PromptReady:          promptReady,
		RuntimeBucket:        ClassifyRuntimeBucket(jobStatus, rt.blueprint.Status, allPhasesDone, promptReady, pendingCount),
	}
	if rt.state != nil && rt.state.LaneID != "" {
		summary.LaneID = rt.state.LaneID
	} else if rt.blueprint.Metadata != nil {
		if v, ok := rt.blueprint.Metadata["lane_id"].(string); ok {
			summary.LaneID = strings.TrimSpace(v)
		}
	}
	if rt.blueprint.Delivery != nil {
		summary.FormerLocation = rt.blueprint.Delivery.FormerLocation
		summary.SignOffMechanism = rt.blueprint.Delivery.SignOffMechanism
	}
	if rt.blueprint.PostCompletion != nil {
		summary.RequiresPostCompletion = true
	}
	if rt.state != nil && rt.state.PostCompletion != nil {
		summary.AssessmentStatus = strings.TrimSpace(rt.state.PostCompletion.AssessmentStatus)
	}
	return summary
}

func (h *Handler) HandleProgramJobs(w http.ResponseWriter, r *http.Request) {
	programID := chi.URLParam(r, "programId")
	h.mu.Lock()
	defer h.mu.Unlock()

	rt, ok := h.runtimes[programID]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "program not found"})
		return
	}

	history := rt.history
	if history == nil {
		history = []Job{}
	}
	writeJSON(w, http.StatusOK, ProgramJobsResponse{
		ProgramID: programID,
		ActiveJob: rt.activeJob,
		History:   history,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
