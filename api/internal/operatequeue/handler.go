package operatequeue

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/remediation"
)

type Handler struct {
	store    *Store
	audit    *actuation.AuditLog
	jobs     *remediation.JobStore
	observer LifecycleObserver
}

type LifecycleObserver interface {
	OnOperateQueueExecution(item Item)
	OnOperateQueueClosed(item Item)
}

func (h *Handler) BindRemediationJobs(jobs *remediation.JobStore) {
	h.jobs = jobs
}

func (h *Handler) BindLifecycleObserver(observer LifecycleObserver) {
	h.observer = observer
}

func NewHandler(configDir string, audit *actuation.AuditLog) *Handler {
	return &Handler{
		store: NewStore(configDir),
		audit: audit,
	}
}

func (h *Handler) Store() *Store {
	return h.store
}

func (h *Handler) HandleGetQueue(w http.ResponseWriter, _ *http.Request) {
	list, err := h.store.List()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) HandleEnqueue(w http.ResponseWriter, r *http.Request) {
	var req EnqueueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	item, err := NewItemFromManual(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	saved, err := h.store.Add(item)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.audit.Record(r, "operate.queue.enqueue", saved.ID, StatusOpen,
		fmt.Sprintf("program=%s source=%s", saved.ProgramID, saved.Source))
	writeJSON(w, http.StatusCreated, saved)
}

// EnqueueChecklistDispatch adds a semi_auto checklist handoff (source=checklist_dispatch).
func (h *Handler) EnqueueChecklistDispatch(req EnqueueRequest) (Item, error) {
	item, err := NewItemFromManual(req)
	if err != nil {
		return Item{}, err
	}
	item.Source = SourceChecklistDispatch
	if strings.TrimSpace(item.Reason) == "" {
		item.Reason = "checklist_dispatch"
	}
	saved, err := h.store.Add(item)
	if err != nil {
		return Item{}, err
	}
	return saved, nil
}

func (h *Handler) HandleClose(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
		return
	}
	var req CloseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	jobDone := false
	if item, ok := h.store.FindByID(id); ok && item.ExecutionJobID != "" && h.jobs != nil {
		if job, found := h.jobs.Get(item.ExecutionJobID); found {
			jobDone = job.Status == remediation.JobDone
		}
	}
	closed, err := h.store.Close(id, req, jobDone)
	if err != nil {
		msg := err.Error()
		switch msg {
		case "item not found":
			writeJSON(w, http.StatusNotFound, map[string]string{"error": msg})
		default:
			writeJSON(w, http.StatusConflict, map[string]string{"error": msg})
		}
		return
	}
	h.audit.Record(r, "operate.queue.close", closed.ID, StatusClosed,
		fmt.Sprintf("program=%s", closed.ProgramID))
	if h.observer != nil {
		h.observer.OnOperateQueueClosed(closed)
	}
	writeJSON(w, http.StatusOK, closed)
}

func (h *Handler) HandleDismiss(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
		return
	}
	var req DismissRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	closed, err := h.store.Dismiss(id, req)
	if err != nil {
		msg := err.Error()
		switch msg {
		case "item not found":
			writeJSON(w, http.StatusNotFound, map[string]string{"error": msg})
		default:
			writeJSON(w, http.StatusConflict, map[string]string{"error": msg})
		}
		return
	}
	h.audit.Record(r, "operate.queue.dismiss", closed.ID, StatusClosed,
		fmt.Sprintf("program=%s", closed.ProgramID))
	if h.observer != nil {
		h.observer.OnOperateQueueClosed(closed)
	}
	writeJSON(w, http.StatusOK, closed)
}

func (h *Handler) HandleRecordExecution(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	var req ExecutionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if h.jobs == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "remediation job store unavailable"})
		return
	}
	if _, ok := h.jobs.Get(strings.TrimSpace(req.ExecutionJobID)); !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "execution job not found"})
		return
	}
	item, err := h.store.RecordExecution(id, req.ExecutionJobID)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	h.audit.Record(r, "operate.queue.execution", item.ID, item.Status, "job="+item.ExecutionJobID)
	if h.observer != nil {
		h.observer.OnOperateQueueExecution(item)
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) InjectFromApproval(r *http.Request, params ApprovalInjectParams) (Item, error) {
	item := NewItemFromApproval(params)
	saved, err := h.store.Add(item)
	if err != nil {
		return Item{}, err
	}
	detail := fmt.Sprintf("program=%s pending=%s", saved.ProgramID, saved.PendingID)
	if strings.TrimSpace(saved.Lane) != "" {
		detail += " lane=" + saved.Lane
	}
	h.audit.Record(r, "operate.queue.enqueue", saved.ID, StatusOpen, detail)
	return saved, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
