package devagent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

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
	CompletedAt   string    `json:"completed_at,omitempty"`
	CursorAgentID string    `json:"cursor_agent_id,omitempty"`
}

type StatusResponse struct {
	Project   string       `json:"project"`
	Program   ProgramInfo  `json:"program"`
	Phases    []Phase      `json:"phases"`
	ActiveJob *Job         `json:"active_job"`
	History   []Job        `json:"history"`
}

type ProgramDetailResponse struct {
	Program ProgramInfo   `json:"program"`
	Phases  []PhaseDetail `json:"phases"`
	Bridge  BridgeConfig  `json:"bridge"`
	Active  bool          `json:"active"`
}

type PhaseDetail struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	Status         string   `json:"status"`
	VerifyCmd      string   `json:"verify_cmd,omitempty"`
	Acceptance     []string `json:"acceptance,omitempty"`
	DependsOn      []string `json:"depends_on,omitempty"`
	RenderedPrompt string   `json:"rendered_prompt,omitempty"`
	SkillInjected  bool     `json:"skill_injected,omitempty"`
}

type BridgeConfig struct {
	Workspace   string `json:"workspace"`
	Model       string `json:"model"`
	SkillPath   string `json:"skill_path,omitempty"`
	SkillLoaded bool   `json:"skill_loaded"`
}

type StartRequest struct {
	PhaseID string `json:"phase_id"`
}

type RejectRequest struct {
	Feedback string `json:"feedback"`
}

type programRuntime struct {
	blueprint *ProgramBlueprint
	phases    []Phase
	activeJob *Job
	history   []Job
	state     *ProgramStateRecord
}

type Handler struct {
	mu              sync.Mutex
	runtimes        map[string]*programRuntime
	activeProgramID string
	blueprintDir    string
	repoRoot        string
	configDir       string
	bridgeCmd       string
	store           *FileStore
	operateQueue    *operatequeue.Handler
	sessionStore    sessionValidator
}

// sessionValidator is the progress-hook surface from api/internal/sessions.
type sessionValidator interface {
	ValidateProgressHook(sessionID, programID, phaseID string) error
}

func (h *Handler) BindOperateQueue(oq *operatequeue.Handler) {
	h.operateQueue = oq
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
		bridgeCmd:    "node",
		repoRoot:     repoRoot,
		configDir:    configDir,
		blueprintDir: programsDir,
		runtimes:     make(map[string]*programRuntime, len(blueprints)),
		store:        store,
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
			rt.phases = mergePhasesFromState(bp, saved.Phases)
			rt.activeJob = saved.ActiveJob
			rt.history = saved.History
			rt.state = saved
			if rt.history == nil {
				rt.history = []Job{}
			}
		} else {
			rt.state = &ProgramStateRecord{ProgramID: bp.ID, History: []Job{}}
		}
		h.runtimes[bp.ID] = rt
	}

	activeID, err := store.LoadActiveProgramID()
	if err != nil {
		return nil, err
	}
	if activeID != "" {
		if _, ok := h.runtimes[activeID]; ok {
			h.activeProgramID = activeID
		}
	}
	if h.activeProgramID == "" {
		h.activeProgramID = pickDefaultActive(blueprints)
	}
	if err := store.SaveActiveProgramID(h.activeProgramID); err != nil {
		return nil, err
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
	if rt.state == nil {
		rt.state = &ProgramStateRecord{ProgramID: programID, History: []Job{}}
	}
	rt.state.ProgramID = programID
	rt.state.Phases = rt.phases
	rt.state.ActiveJob = rt.activeJob
	rt.state.History = rt.history
	return h.store.SaveProgramRecord(rt.state)
}

func (h *Handler) persistActiveLocked() error {
	return h.store.SaveActiveProgramID(h.activeProgramID)
}

func pickDefaultActive(blueprints []*ProgramBlueprint) string {
	for _, bp := range blueprints {
		if bp.ID == "trade-ib-migration" || bp.ID == "trade-ib-client-migration" {
			return bp.ID
		}
	}
	for _, bp := range blueprints {
		if bp.Status == "active" && bp.Workspace != "" {
			return bp.ID
		}
	}
	return blueprints[0].ID
}

func (h *Handler) activeRuntime() *programRuntime {
	return h.runtimes[h.activeProgramID]
}

func (h *Handler) HandlePrograms(w http.ResponseWriter, r *http.Request) {
	boardOnly := r.URL.Query().Get("board") == "1" || r.URL.Query().Get("board") == "true"
	templateFilter := strings.TrimSpace(r.URL.Query().Get("template_id"))
	laneFilter := strings.TrimSpace(r.URL.Query().Get("lane_id"))
	h.mu.Lock()
	defer h.mu.Unlock()

	list := make([]ProgramSummary, 0, len(h.runtimes))
	for id, rt := range h.runtimes {
		if boardOnly && (rt.blueprint.Delivery == nil || !rt.blueprint.Delivery.BoardVisible) {
			continue
		}
		if templateFilter != "" && templateIDFromRuntime(rt) != templateFilter {
			continue
		}
		sum := h.buildProgramSummary(id, rt)
		if laneFilter != "" && sum.LaneID != laneFilter {
			continue
		}
		list = append(list, sum)
	}
	writeJSON(w, http.StatusOK, map[string]any{"programs": list})
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
		AllPhasesDone:        phaseCount > 0 && phasesDone == phaseCount,
		Active:               programID == h.activeProgramID,
		Delivery:             rt.blueprint.Delivery,
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

func (h *Handler) HandlePersistence(w http.ResponseWriter, _ *http.Request) {
	h.mu.Lock()
	activeID := h.activeProgramID
	h.mu.Unlock()

	info, err := h.store.ListInfo(activeID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (h *Handler) HandleActivateProgram(w http.ResponseWriter, r *http.Request) {
	programID := chi.URLParam(r, "programId")
	h.mu.Lock()
	defer h.mu.Unlock()

	rt, ok := h.runtimes[programID]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "program not found"})
		return
	}
	if rt.activeJob != nil && rt.activeJob.Status == JobRunning {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "cannot switch program while agent is running"})
		return
	}

	h.activeProgramID = programID
	if err := h.persistActiveLocked(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, h.programDetailBoardResponse(programID, rt))
}

func (h *Handler) HandleStatus(w http.ResponseWriter, _ *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	rt := h.activeRuntime()
	if rt == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "no active program"})
		return
	}

	bp := rt.blueprint
	resp := StatusResponse{
		Project: bp.ID,
		Program: ProgramInfo{
			ID:          bp.ID,
			Title:       bp.Title,
			Description: bp.Description,
			Status:      bp.Status,
		},
		Phases:    rt.phases,
		ActiveJob: rt.activeJob,
		History:   rt.history,
	}
	if resp.History == nil {
		resp.History = []Job{}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleStart(w http.ResponseWriter, r *http.Request) {
	var req StartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	h.mu.Lock()
	rt := h.activeRuntime()
	if rt == nil {
		h.mu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "no active program"})
		return
	}
	if rt.activeJob != nil && rt.activeJob.Status == JobRunning {
		h.mu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "agent already running"})
		return
	}

	var targetPhase *Phase
	for i := range rt.phases {
		if rt.phases[i].ID == req.PhaseID {
			targetPhase = &rt.phases[i]
			break
		}
	}
	if targetPhase == nil {
		h.mu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "phase not found"})
		return
	}

	job := &Job{
		ID:      uuid.New().String(),
		PhaseID: req.PhaseID,
		Status:  JobRunning,
		Output:  "",
	}
	targetPhase.Status = PhaseRunning
	targetPhase.StartedAt = time.Now().UTC().Format(time.RFC3339)
	rt.activeJob = job
	programID := h.activeProgramID
	if err := h.persistRuntimeLocked(programID); err != nil {
		h.mu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.mu.Unlock()

	go h.runBridge(programID, job)

	writeJSON(w, http.StatusAccepted, job)
}

func (h *Handler) HandleApprove(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.mu.Lock()

	rt := h.activeRuntime()
	if rt == nil || rt.activeJob == nil || rt.activeJob.ID != id {
		h.mu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}

	rt.activeJob.Status = JobDone
	rt.activeJob.CompletedAt = time.Now().UTC().Format(time.RFC3339)

	rt.history = append([]Job{*rt.activeJob}, rt.history...)
	archived := *rt.activeJob
	phaseID := archived.PhaseID
	programID := h.activeProgramID
	rt.activeJob = nil
	if err := h.persistRuntimeLocked(programID); err != nil {
		h.mu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.mu.Unlock()

	by := actuation.PrincipalFromContext(r.Context()).Name
	if by == "" {
		by = "owner"
	}
	notes := "Dev Agent phase verified"
	if err := h.RecordPhaseSignoff(programID, phaseID, by, archived.CompletedAt, notes); err != nil {
		if strings.Contains(err.Error(), "already signed off") {
			writeJSON(w, http.StatusOK, archived)
			return
		}
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, archived)
}

func (h *Handler) HandleReject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req RejectRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	h.mu.Lock()
	rt := h.activeRuntime()
	if rt == nil || rt.activeJob == nil || rt.activeJob.ID != id {
		h.mu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}

	rt.activeJob.Status = JobRunning
	rt.activeJob.Output += fmt.Sprintf("\n\n--- Owner feedback ---\n%s\n--- Resuming agent ---\n", req.Feedback)
	programID := h.activeProgramID
	job := rt.activeJob
	if err := h.persistRuntimeLocked(programID); err != nil {
		h.mu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.mu.Unlock()

	go h.resumeBridge(programID, job, req.Feedback)

	writeJSON(w, http.StatusOK, job)
}

func (h *Handler) HandleCancel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.mu.Lock()
	defer h.mu.Unlock()

	rt := h.activeRuntime()
	if rt == nil || rt.activeJob == nil || rt.activeJob.ID != id {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}

	rt.activeJob.Status = JobCancelled
	rt.activeJob.CompletedAt = time.Now().UTC().Format(time.RFC3339)

	for i := range rt.phases {
		if rt.phases[i].ID == rt.activeJob.PhaseID {
			rt.phases[i].Status = PhasePending
			break
		}
	}

	rt.history = append([]Job{*rt.activeJob}, rt.history...)
	archived := *rt.activeJob
	rt.activeJob = nil
	if err := h.persistRuntimeLocked(h.activeProgramID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, archived)
}

func (h *Handler) bridgeScriptPath() string {
	return filepath.Join(h.repoRoot, "scripts", "dev-agent", "dist", "bridge.js")
}

func (h *Handler) bridgeArgs(bp *ProgramBlueprint, phaseID string, extra ...string) []string {
	prompt := promptForPhase(bp, phaseID)
	args := []string{
		h.bridgeScriptPath(),
		"--prompt", prompt,
		"--phase", phaseID,
		"--workspace", bp.Workspace,
		"--model", bp.Model,
	}
	if strings.TrimSpace(bp.SkillPath) != "" {
		args = append(args, "--skill-path", bp.SkillPath)
	}
	args = append(args, extra...)
	return args
}

func (h *Handler) runBridgeCommand(args []string) ([]byte, error) {
	cmd := exec.Command(h.bridgeCmd, args...)
	cmd.Dir = h.repoRoot
	return cmd.CombinedOutput()
}

func (h *Handler) runBridge(programID string, job *Job) {
	h.mu.Lock()
	rt := h.runtimes[programID]
	if rt == nil {
		h.mu.Unlock()
		return
	}
	bp := rt.blueprint
	phaseID := job.PhaseID
	h.mu.Unlock()

	out, err := h.runBridgeCommand(h.bridgeArgs(bp, phaseID))

	h.mu.Lock()
	defer h.mu.Unlock()

	rt = h.runtimes[programID]
	if rt == nil || rt.activeJob == nil || rt.activeJob.ID != job.ID {
		return
	}

	if err != nil {
		rt.activeJob.Output += string(out) + "\n[Bridge error: " + err.Error() + "]"
		rt.activeJob.Status = JobAwaitingReview
	} else {
		rt.activeJob.Output = string(out)
		rt.activeJob.Status = JobAwaitingReview
	}
	_ = h.persistRuntimeLocked(programID)
}

func (h *Handler) resumeBridge(programID string, job *Job, feedback string) {
	h.mu.Lock()
	rt := h.runtimes[programID]
	if rt == nil {
		h.mu.Unlock()
		return
	}
	bp := rt.blueprint
	h.mu.Unlock()

	prompt := fmt.Sprintf("Owner requested changes: %s\nPlease fix and re-verify.", feedback)
	args := []string{
		h.bridgeScriptPath(),
		"--resume", job.CursorAgentID,
		"--prompt", prompt,
		"--workspace", bp.Workspace,
		"--model", bp.Model,
	}
	if strings.TrimSpace(bp.SkillPath) != "" {
		args = append(args, "--skill-path", bp.SkillPath)
	}

	out, err := h.runBridgeCommand(args)

	h.mu.Lock()
	defer h.mu.Unlock()

	rt = h.runtimes[programID]
	if rt == nil || rt.activeJob == nil || rt.activeJob.ID != job.ID {
		return
	}

	if err != nil {
		rt.activeJob.Output += string(out) + "\n[Bridge error: " + err.Error() + "]"
	} else {
		rt.activeJob.Output += string(out)
	}
	rt.activeJob.Status = JobAwaitingReview
	_ = h.persistRuntimeLocked(programID)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
