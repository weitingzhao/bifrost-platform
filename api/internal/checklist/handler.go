package checklist

import (
	"encoding/json"
	"net/http"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/operatequeue"
	"github.com/weitingzhao/bifrost-platform/api/internal/remediation"
)

type Handler struct {
	store       *Store
	audit       *actuation.AuditLog
	remediation *remediation.Handler
	operate     *operatequeue.Handler
}

func NewHandler(configDir string, audit *actuation.AuditLog) *Handler {
	return &Handler{
		store: NewStore(configDir),
		audit: audit,
	}
}

func (h *Handler) BindRemediation(r *remediation.Handler)   { h.remediation = r }
func (h *Handler) BindOperateQueue(o *operatequeue.Handler) { h.operate = o }

// Store exposes the checklist signal cache for read-only consumers (e.g. queue sweep).
func (h *Handler) Store() *Store { return h.store }

func (h *Handler) HandleGetSignals(w http.ResponseWriter, _ *http.Request) {
	resp, err := h.store.Get()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleGetKPIs(w http.ResponseWriter, _ *http.Request) {
	resp, err := h.store.KPIs()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandlePostSignals(w http.ResponseWriter, r *http.Request) {
	var req MergeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if len(req.Signals) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "signals required"})
		return
	}

	resp, err := h.store.Merge(req)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.audit.Record(r, "checklist.signals.merge", req.RunID, "ok",
		"count="+itoa(len(req.Signals)))

	if req.AutoDispatch && h.remediation != nil && h.operate != nil {
		actions := h.executeDispatch(r.Context(), resp.Signals)
		_ = h.store.SetDispatch(actions)
		resp.LastDispatch = actions
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) lastDispatchSnapshot() []DispatchAction {
	resp, err := h.store.Get()
	if err != nil {
		return nil
	}
	return resp.LastDispatch
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
