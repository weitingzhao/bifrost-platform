package lanes

import (
	"encoding/json"
	"fmt"
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
	list, err := h.store.List()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) HandleGet(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
		return
	}
	lane, ok, err := h.store.Get(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "lane not found"})
		return
	}
	writeJSON(w, http.StatusOK, lane)
}

func (h *Handler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	lane := req.ToLane()
	created, err := h.store.Create(lane)
	if err != nil {
		if IsValidation(err) {
			status := http.StatusBadRequest
			if strings.HasPrefix(err.Error(), "lane already exists") {
				status = http.StatusConflict
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if h.audit != nil {
		h.audit.Record(r, "lanes.create", created.ID, "created",
			fmt.Sprintf("track=%s line=%s", created.Track, created.ComponentLine))
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h *Handler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
		return
	}
	var req UpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	updated, err := h.store.Update(id, req)
	if err != nil {
		if IsValidation(err) {
			status := http.StatusBadRequest
			if strings.HasPrefix(err.Error(), "lane not found") {
				status = http.StatusNotFound
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if h.audit != nil {
		h.audit.Record(r, "lanes.update", updated.ID, "updated",
			fmt.Sprintf("track=%s line=%s track_type=%s", updated.Track, updated.ComponentLine, updated.TrackType))
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
		return
	}
	if err := h.store.Delete(id); err != nil {
		if IsValidation(err) {
			status := http.StatusBadRequest
			if strings.HasPrefix(err.Error(), "lane not found") {
				status = http.StatusNotFound
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if h.audit != nil {
		h.audit.Record(r, "lanes.delete", id, "deleted", "removed from lanes.yaml")
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": "deleted"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
