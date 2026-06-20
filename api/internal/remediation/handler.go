package remediation

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

type Handler struct {
	runner *RunnerClient
	audit  *actuation.AuditLog
	store  *Store
}

func NewHandler(audit *actuation.AuditLog) *Handler {
	return &Handler{
		runner: NewRunnerClient(),
		audit:  audit,
		store:  NewStore(),
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

	job, err := h.runner.Start(r.Context(), runReq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error":   "remediation runner unavailable",
			"detail":  err.Error(),
			"hint":    "Start sidecar: make dev-agent",
		})
		return
	}

	job.Actor = principal.Name
	job.Scope = req.Scope
	h.store.Put(*job)

	h.audit.Record(r, "remediation.start", job.ID, "started", req.Scope)

	writeJSON(w, http.StatusAccepted, job)
}

func (h *Handler) HandleGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	job, err := h.runner.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if job == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (h *Handler) HandleList(w http.ResponseWriter, r *http.Request) {
	jobs, err := h.runner.List(r.Context())
	if err != nil {
		// Fall back to local store when runner is down.
		writeJSON(w, http.StatusOK, map[string]any{"jobs": h.store.List()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

func (h *Handler) HandleCancel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	job, err := h.runner.Cancel(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
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
			h.store.Put(*envelope.Job)
			if envelope.Job.Status == JobDone {
				h.audit.RecordDirect(
					envelope.Job.Actor,
					actuation.RoleOperator,
					"remediation.done",
					envelope.Job.ID,
					"done",
					envelope.Job.Summary,
				)
			} else if envelope.Job.Status == JobFailed {
				h.audit.RecordDirect(
					envelope.Job.Actor,
					actuation.RoleOperator,
					"remediation.failed",
					envelope.Job.ID,
					"failed",
					envelope.Job.Error,
				)
			}
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

type Store struct {
	jobs map[string]Job
}

func NewStore() *Store {
	return &Store{jobs: make(map[string]Job)}
}

func (s *Store) Put(job Job) {
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now().UTC()
	}
	job.UpdatedAt = time.Now().UTC()
	s.jobs[job.ID] = job
}

func (s *Store) List() []Job {
	out := make([]Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		out = append(out, j)
	}
	return out
}
