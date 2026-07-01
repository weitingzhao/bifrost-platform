package promote

import (
	"encoding/json"
	"net/http"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

type Handler struct {
	svc   *Service
	store *Store
	audit *actuation.AuditLog
}

func NewHandler(cfg *config.Config, audit *actuation.AuditLog, cluster *cluster.Handler) *Handler {
	svc := NewService(cfg, cluster)
	return &Handler{svc: svc, store: svc.store, audit: audit}
}

func (h *Handler) Store() *Store {
	return h.store
}

func (h *Handler) tierFromRequest(r *http.Request) GateTier {
	return ParseGateTier(r.URL.Query().Get("tier"))
}

func (h *Handler) HandleGetReleaseGate(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.LastGate(r.Context(), h.tierFromRequest(r)))
}

func (h *Handler) HandleRunReleaseGate(w http.ResponseWriter, r *http.Request) {
	tier := h.tierFromRequest(r)
	principal := actuation.PrincipalFromContext(r.Context())
	resp, err := h.svc.RunReleaseGate(r.Context(), tier, principal.Name)
	status := "ok"
	if err != nil {
		status = "failed"
	}
	if h.audit != nil {
		msg := resp.Message
		if err != nil {
			msg = err.Error()
		}
		h.audit.Record(r, resp.Action, resp.Target, status, msg)
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"ok":      false,
			"action":  "promote.release-gate",
			"message": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleGetGateHistory(w http.ResponseWriter, r *http.Request) {
	tier := h.tierFromRequest(r)
	history, err := h.svc.GateHistory(tier)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": err.Error(),
		})
		return
	}
	if history == nil {
		history = []ReleaseGateRecord{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tier":    tier,
		"entries": history,
	})
}

func (h *Handler) HandleGetReleaseState(w http.ResponseWriter, r *http.Request) {
	tier := r.URL.Query().Get("tier")
	if tier == "" {
		tier = "platform"
	}
	writeJSON(w, http.StatusOK, h.svc.ReleaseState(r.Context(), tier))
}

func (h *Handler) HandleGetTierB(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.TierBStatus(r.Context()))
}

type tierBSignoffRequest struct {
	Notes string `json:"notes"`
}

func (h *Handler) HandleSignTierB(w http.ResponseWriter, r *http.Request) {
	var req tierBSignoffRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	principal := actuation.PrincipalFromContext(r.Context())
	resp, err := h.svc.SignTierB(r.Context(), req.Notes, principal.Name)
	status := "ok"
	if err != nil {
		status = "failed"
	}
	if h.audit != nil {
		msg := resp.Message
		if err != nil {
			msg = err.Error()
		}
		h.audit.Record(r, resp.Action, resp.Target, status, msg)
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok":      false,
			"action":  "promote.tier-b-signoff",
			"message": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
