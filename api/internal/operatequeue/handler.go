package operatequeue

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

type Handler struct {
	store *Store
	audit *actuation.AuditLog
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
