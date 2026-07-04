package devagent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
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
	Project   string  `json:"project"`
	Phases    []Phase `json:"phases"`
	ActiveJob *Job    `json:"active_job"`
	History   []Job   `json:"history"`
}

type StartRequest struct {
	PhaseID string `json:"phase_id"`
}

type RejectRequest struct {
	Feedback string `json:"feedback"`
}

type Handler struct {
	mu        sync.Mutex
	phases    []Phase
	activeJob *Job
	history   []Job
	bridgeCmd string
}

func NewHandler() *Handler {
	h := &Handler{
		bridgeCmd: "node",
		phases: []Phase{
			{ID: "TIBM0", Title: "Inventory & sign-off", Status: PhaseDone},
			{ID: "TIBM1", Title: "Gateway RPC parity", Status: PhaseDone},
			{ID: "TIBM2", Title: "Trade read-path + health", Status: PhaseDone},
			{ID: "TIBM3", Title: "Workers RPC-only", Status: PhaseDone},
			{ID: "TIBM4", Title: "UI + legacy cleanup", Status: PhasePending},
		},
	}
	return h
}

func (h *Handler) HandleStatus(w http.ResponseWriter, _ *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	resp := StatusResponse{
		Project:   "trade-ib-client-migration",
		Phases:    h.phases,
		ActiveJob: h.activeJob,
		History:   h.history,
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
	if h.activeJob != nil && h.activeJob.Status == JobRunning {
		h.mu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "agent already running"})
		return
	}

	var targetPhase *Phase
	for i := range h.phases {
		if h.phases[i].ID == req.PhaseID {
			targetPhase = &h.phases[i]
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
	h.activeJob = job
	h.mu.Unlock()

	go h.runBridge(job)

	writeJSON(w, http.StatusAccepted, job)
}

func (h *Handler) HandleApprove(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.activeJob == nil || h.activeJob.ID != id {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}

	h.activeJob.Status = JobDone
	h.activeJob.CompletedAt = time.Now().UTC().Format(time.RFC3339)

	for i := range h.phases {
		if h.phases[i].ID == h.activeJob.PhaseID {
			h.phases[i].Status = PhaseDone
			h.phases[i].CompletedAt = h.activeJob.CompletedAt
			break
		}
	}

	h.history = append([]Job{*h.activeJob}, h.history...)
	archived := *h.activeJob
	h.activeJob = nil

	writeJSON(w, http.StatusOK, archived)
}

func (h *Handler) HandleReject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req RejectRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.activeJob == nil || h.activeJob.ID != id {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}

	h.activeJob.Status = JobRunning
	h.activeJob.Output += fmt.Sprintf("\n\n--- Owner feedback ---\n%s\n--- Resuming agent ---\n", req.Feedback)

	go h.resumeBridge(h.activeJob, req.Feedback)

	writeJSON(w, http.StatusOK, h.activeJob)
}

func (h *Handler) HandleCancel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.activeJob == nil || h.activeJob.ID != id {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}

	h.activeJob.Status = JobCancelled
	h.activeJob.CompletedAt = time.Now().UTC().Format(time.RFC3339)

	for i := range h.phases {
		if h.phases[i].ID == h.activeJob.PhaseID {
			h.phases[i].Status = PhasePending
			break
		}
	}

	h.history = append([]Job{*h.activeJob}, h.history...)
	archived := *h.activeJob
	h.activeJob = nil

	writeJSON(w, http.StatusOK, archived)
}

func (h *Handler) runBridge(job *Job) {
	h.mu.Lock()
	phaseID := job.PhaseID
	h.mu.Unlock()

	prompt := fmt.Sprintf(
		"Execute IB Migration %s. Follow .cursor/skills/ib-migration/SKILL.md and "+
			"update bifrost-trade-infra/docs/IB_MIGRATION_PROGRESS.md when complete. "+
			"Output a structured Phase completion report at the end.",
		phaseID,
	)

	out, err := exec.Command(
		h.bridgeCmd,
		"scripts/dev-agent/bridge.js",
		"--prompt", prompt,
		"--phase", phaseID,
	).CombinedOutput()

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.activeJob == nil || h.activeJob.ID != job.ID {
		return
	}

	if err != nil {
		h.activeJob.Output += string(out) + "\n[Bridge error: " + err.Error() + "]"
		h.activeJob.Status = JobAwaitingReview
	} else {
		h.activeJob.Output = string(out)
		h.activeJob.Status = JobAwaitingReview
	}
}

func (h *Handler) resumeBridge(job *Job, feedback string) {
	prompt := fmt.Sprintf("Owner requested changes: %s\nPlease fix and re-verify.", feedback)

	out, err := exec.Command(
		h.bridgeCmd,
		"scripts/dev-agent/bridge.js",
		"--resume", job.CursorAgentID,
		"--prompt", prompt,
	).CombinedOutput()

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.activeJob == nil || h.activeJob.ID != job.ID {
		return
	}

	if err != nil {
		h.activeJob.Output += string(out) + "\n[Bridge error: " + err.Error() + "]"
	} else {
		h.activeJob.Output += string(out)
	}
	h.activeJob.Status = JobAwaitingReview
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
