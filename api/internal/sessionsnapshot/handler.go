package sessionsnapshot

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

type Handler struct {
	store *Store
}

func NewHandler() *Handler {
	return &Handler{store: NewStore()}
}

func (h *Handler) HandleLatest(w http.ResponseWriter, _ *http.Request) {
	env, ok := h.store.Latest()
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"snapshot": nil})
		return
	}
	var payload any
	_ = json.Unmarshal(env.Payload, &payload)
	writeJSON(w, http.StatusOK, map[string]any{
		"snapshot": payload,
		"saved_at": env.SavedAt,
		"saved_by": env.SavedBy,
	})
}

func (h *Handler) HandleSave(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body"})
		return
	}
	if !json.Valid(body) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	principal := actuation.PrincipalFromContext(r.Context())
	env := Envelope{
		SavedAt: time.Now().UTC().Format(time.RFC3339),
		SavedBy: principal.Name,
		Payload: json.RawMessage(body),
	}
	if err := h.store.Save(env); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"saved_at": env.SavedAt,
		"saved_by": env.SavedBy,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
