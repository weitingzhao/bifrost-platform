package remediation

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

type Handler struct {
	runner     *RunnerClient
	audit      *actuation.AuditLog
	store      *JobStore
	onTerminal TerminalObserver
}

// TerminalObserver is notified when a remediation job reaches a terminal status.
type TerminalObserver interface {
	OnRemediationTerminal(job Job)
}

func NewHandler(audit *actuation.AuditLog) *Handler {
	return &Handler{
		runner: NewRunnerClient(),
		audit:  audit,
		store:  NewJobStore(),
	}
}

// Store returns the underlying JobStore for cross-package consumers (e.g. retrospective analyzer).
func (h *Handler) Store() *JobStore { return h.store }

func (h *Handler) BindTerminalObserver(obs TerminalObserver) {
	h.onTerminal = obs
}

func (h *Handler) notifyTerminal(job *Job) {
	if h.onTerminal == nil || job == nil {
		return
	}
	switch job.Status {
	case JobDone, JobFailed, JobCancelled:
		h.onTerminal.OnRemediationTerminal(*job)
	}
}

func (h *Handler) HandleStart(w http.ResponseWriter, r *http.Request) {
	var req StartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}

	principal := actuation.PrincipalFromContext(r.Context())
	runReq := StartRunnerRequest{
		Scope:            req.Scope,
		Actor:            principal.Name,
		ClusterSummary:   req.ClusterSummary,
		ServiceReadiness: req.ServiceReadiness,
		Governance:       req.Governance,
		Issues:           req.Issues,
		Prompt:           req.Prompt,
	}

	job, err := h.StartInternal(r.Context(), runReq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error":  "remediation runner unavailable",
			"detail": err.Error(),
			"hint":   "Start sidecar: make dev-agent",
		})
		return
	}

	h.audit.Record(r, "remediation.start", job.ID, "started", req.Scope)

	writeJSON(w, http.StatusAccepted, job)
}

// StartInternal starts a runner job and archives it (used by HTTP + checklist dispatch).
func (h *Handler) StartInternal(ctx context.Context, runReq StartRunnerRequest) (*Job, error) {
	job, err := h.runner.Start(ctx, runReq)
	if err != nil {
		return nil, err
	}
	if runReq.Actor != "" {
		job.Actor = runReq.Actor
	}
	job.Scope = runReq.Scope
	h.store.Put(*job)
	return job, nil
}

func (h *Handler) HandleGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	job, err := h.runner.Get(r.Context(), id)
	if err != nil {
		if stored, ok := h.store.Get(id); ok {
			writeJSON(w, http.StatusOK, stored)
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if job == nil {
		if stored, ok := h.store.Get(id); ok {
			writeJSON(w, http.StatusOK, stored)
			return
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	prev, _ := h.store.Get(id)
	h.store.PutMergingEvents(*job)
	if prev == nil || prev.Status != job.Status {
		h.notifyTerminal(job)
	}
	if stored, ok := h.store.Get(id); ok {
		writeJSON(w, http.StatusOK, stored)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (h *Handler) HandleList(w http.ResponseWriter, r *http.Request) {
	stored := h.store.List()
	jobs, err := h.runner.List(r.Context())
	if err != nil {
		reconciled := ReconcileOrphanedJobs(nil, stored)
		for _, j := range reconciled {
			h.store.Put(j)
		}
		writeJSON(w, http.StatusOK, map[string]any{"jobs": reconciled, "source": "archive"})
		return
	}
	merged := ReconcileOrphanedJobs(jobs, stored)
	for _, j := range merged {
		h.store.Put(j)
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": merged})
}

func (h *Handler) HandleCancel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	job, err := h.runner.Cancel(r.Context(), id)
	if err != nil {
		if stored, ok := h.store.Get(id); ok && stored.Status == JobRunning {
			now := time.Now().UTC()
			stored.Status = JobCancelled
			stored.Phase = PhaseCancelled
			stored.Summary = orphanJobSummary
			stored.Error = "orphaned"
			stored.UpdatedAt = now
			h.store.Put(*stored)
			h.audit.Record(r, "remediation.cancel", id, "orphan_dismissed", "")
			writeJSON(w, http.StatusOK, stored)
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	h.store.Put(*job)
	h.audit.Record(r, "remediation.cancel", id, "cancelled", "")
	writeJSON(w, http.StatusOK, job)
}

func (h *Handler) HandleRespond(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req RespondRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if req.OptionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "option_id required"})
		return
	}
	if err := h.runner.Respond(r.Context(), id, req); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	h.audit.Record(r, "remediation.respond", id, req.OptionID, req.Note)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) HandleStream(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	err := h.runner.Stream(r.Context(), id, func(payload []byte) error {
		var envelope struct {
			Type  string          `json:"type"`
			Job   *Job            `json:"job"`
			Event *Event          `json:"event"`
			Raw   json.RawMessage `json:"-"`
		}
		if err := json.Unmarshal(payload, &envelope); err != nil {
			return nil
		}
		if envelope.Type == "job" && envelope.Job != nil {
			prev, _ := h.store.Get(envelope.Job.ID)
			h.store.PutMergingEvents(*envelope.Job)
			switch envelope.Job.Status {
			case JobDone:
				h.audit.RecordDirect(
					envelope.Job.Actor,
					actuation.RoleOperator,
					"remediation.done",
					envelope.Job.ID,
					"done",
					envelope.Job.Summary,
				)
			case JobFailed:
				h.audit.RecordDirect(
					envelope.Job.Actor,
					actuation.RoleOperator,
					"remediation.failed",
					envelope.Job.ID,
					"failed",
					envelope.Job.Error,
				)
			}
			if prev == nil || prev.Status != envelope.Job.Status {
				h.notifyTerminal(envelope.Job)
			}
		} else if envelope.Type == "event" && envelope.Event != nil {
			h.store.AppendEvent(id, *envelope.Event)
		}
		_, writeErr := w.Write([]byte("data: " + string(payload) + "\n\n"))
		if writeErr != nil {
			return writeErr
		}
		flusher.Flush()
		return nil
	})
	if err != nil && r.Context().Err() == nil && !errors.Is(err, io.EOF) {
		fail := map[string]string{"type": "error", "text": err.Error()}
		raw, _ := json.Marshal(fail)
		_, _ = w.Write([]byte("data: " + string(raw) + "\n\n"))
		flusher.Flush()
	}
}

func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	out, err := h.runner.Health(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "unavailable",
			"error":  err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
