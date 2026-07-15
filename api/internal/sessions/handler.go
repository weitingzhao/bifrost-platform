package sessions

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
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

func (h *Handler) Store() *Store { return h.store }

func (h *Handler) HandleList(w http.ResponseWriter, _ *http.Request) {
	list, err := h.store.List(100)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, ListResponse{Sessions: list})
}

func (h *Handler) HandleGet(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
		return
	}
	rec, ok, err := h.store.Get(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (h *Handler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	rec, err := h.store.Create(req)
	if err != nil {
		status := http.StatusBadRequest
		if strings.HasPrefix(err.Error(), "session already exists") {
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	if h.audit != nil {
		h.audit.Record(r, "sessions.create", rec.SessionID, rec.Status,
			"program="+rec.ProgramID+" phase="+rec.PhaseID)
	}
	writeJSON(w, http.StatusCreated, rec)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
