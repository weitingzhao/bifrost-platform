package devagent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
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
	Program ProgramInfo      `json:"program"`
	Phases  []PhaseDetail    `json:"phases"`
	Active  bool             `json:"active"`
}

type PhaseDetail struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Status     string   `json:"status"`
	VerifyCmd  string   `json:"verify_cmd,omitempty"`
	Acceptance []string `json:"acceptance,omitempty"`
	DependsOn  []string `json:"depends_on,omitempty"`
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
}

type Handler struct {
	mu              sync.Mutex
	runtimes        map[string]*programRuntime
	activeProgramID string
	blueprintDir    string
	bridgeCmd       string
}

func NewHandler(configDir string) (*Handler, error) {
	programsDir := filepath.Join(configDir, "programs")
	blueprints, err := LoadProgramBlueprints(programsDir)
	if err != nil {
		return nil, err
	}

	h := &Handler{
		bridgeCmd: "node",
		runtimes:  make(map[string]*programRuntime, len(blueprints)),
	}

	for _, bp := range blueprints {
		h.runtimes[bp.ID] = &programRuntime{
			blueprint: bp,
			phases:    phasesFromBlueprint(bp),
			history:   []Job{},
		}
	}

	h.activeProgramID = pickDefaultActive(blueprints)
	h.blueprintDir = programsDir
	return h, nil
}

func pickDefaultActive(blueprints []*ProgramBlueprint) string {
	for _, bp := range blueprints {
		if bp.ID == "trade-ib-client-migration" {
			return bp.ID
		}
	}
	for _, bp := range blueprints {
		if bp.Status == "active" {
			return bp.ID
		}
	}
	return blueprints[0].ID
}

func (h *Handler) activeRuntime() *programRuntime {
	return h.runtimes[h.activeProgramID]
}

func (h *Handler) HandlePrograms(w http.ResponseWriter, _ *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	list := make([]ProgramSummary, 0, len(h.runtimes))
	for id, rt := range h.runtimes {
		list = append(list, ProgramSummary{
			ID:          id,
			Title:       rt.blueprint.Title,
			Description: rt.blueprint.Description,
			Status:      rt.blueprint.Status,
			PhaseCount:  len(rt.blueprint.Phases),
			Active:      id == h.activeProgramID,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"programs": list})
}

func (h *Handler) HandleGetProgram(w http.ResponseWriter, r *http.Request) {
	programID := chi.URLParam(r, "programId")
	h.mu.Lock()
	defer h.mu.Unlock()

	rt, ok := h.runtimes[programID]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "program not found"})
		return
	}

	writeJSON(w, http.StatusOK, h.programDetailResponse(programID, rt))
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
	writeJSON(w, http.StatusOK, h.programDetailResponse(programID, rt))
}

func (h *Handler) programDetailResponse(programID string, rt *programRuntime) ProgramDetailResponse {
	phases := make([]PhaseDetail, len(rt.blueprint.Phases))
	statusByID := make(map[string]PhaseStatus, len(rt.phases))
	for _, p := range rt.phases {
		statusByID[p.ID] = p.Status
	}
	for i, bp := range rt.blueprint.Phases {
		st := string(parsePhaseStatus(bp.Status))
		if runtimeSt, ok := statusByID[bp.ID]; ok {
			st = string(runtimeSt)
		}
		phases[i] = PhaseDetail{
			ID:         bp.ID,
			Title:      bp.Title,
			Status:     st,
			VerifyCmd:  bp.VerifyCmd,
			Acceptance: bp.Acceptance,
			DependsOn:  bp.DependsOn,
		}
	}
	return ProgramDetailResponse{
		Program: ProgramInfo{
			ID:          programID,
			Title:       rt.blueprint.Title,
			Description: rt.blueprint.Description,
			Status:      rt.blueprint.Status,
		},
		Phases: phases,
		Active: programID == h.activeProgramID,
	}
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
	h.mu.Unlock()

	go h.runBridge(programID, job)

	writeJSON(w, http.StatusAccepted, job)
}

func (h *Handler) HandleApprove(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.mu.Lock()
	defer h.mu.Unlock()

	rt := h.activeRuntime()
	if rt == nil || rt.activeJob == nil || rt.activeJob.ID != id {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}

	rt.activeJob.Status = JobDone
	rt.activeJob.CompletedAt = time.Now().UTC().Format(time.RFC3339)

	for i := range rt.phases {
		if rt.phases[i].ID == rt.activeJob.PhaseID {
			rt.phases[i].Status = PhaseDone
			rt.phases[i].CompletedAt = rt.activeJob.CompletedAt
			break
		}
	}

	rt.history = append([]Job{*rt.activeJob}, rt.history...)
	archived := *rt.activeJob
	rt.activeJob = nil

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

	writeJSON(w, http.StatusOK, archived)
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

	prompt := promptForPhase(bp, phaseID)

	out, err := exec.Command(
		h.bridgeCmd,
		"scripts/dev-agent/bridge.js",
		"--prompt", prompt,
		"--phase", phaseID,
	).CombinedOutput()

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
}

func (h *Handler) resumeBridge(programID string, job *Job, feedback string) {
	prompt := fmt.Sprintf("Owner requested changes: %s\nPlease fix and re-verify.", feedback)

	out, err := exec.Command(
		h.bridgeCmd,
		"scripts/dev-agent/bridge.js",
		"--resume", job.CursorAgentID,
		"--prompt", prompt,
	).CombinedOutput()

	h.mu.Lock()
	defer h.mu.Unlock()

	rt := h.runtimes[programID]
	if rt == nil || rt.activeJob == nil || rt.activeJob.ID != job.ID {
		return
	}

	if err != nil {
		rt.activeJob.Output += string(out) + "\n[Bridge error: " + err.Error() + "]"
	} else {
		rt.activeJob.Output += string(out)
	}
	rt.activeJob.Status = JobAwaitingReview
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
