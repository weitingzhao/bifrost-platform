package ibgateway

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
)

type Handler struct {
	svc   *Service
	audit *actuation.AuditLog
}

func NewHandler(clusterSvc *cluster.Service, audit *actuation.AuditLog) *Handler {
	svc := NewService(clusterSvc)
	h := &Handler{svc: svc, audit: audit}
	svc.StartAutoRepair(context.Background(), audit)
	return h
}

func (h *Handler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.Status(r.Context()))
}

func (h *Handler) HandleSelfHeal(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.SelfHealStatus(r.Context()))
}

func (h *Handler) HandleControl(w http.ResponseWriter, r *http.Request) {
	action := strings.TrimSpace(chi.URLParam(r, "action"))
	switch action {
	case "reconnect":
		h.handleReconnect(w, r)
	case "maintenance":
		h.handleMaintenance(w, r)
	case "mode":
		h.handleMode(w, r)
	case "self-heal":
		h.handleSelfHealToggle(w, r)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + action})
	}
}

func (h *Handler) handleSelfHealToggle(w http.ResponseWriter, r *http.Request) {
	var req ControlRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	resp, err := h.svc.SetSelfHealEnabled(r.Context(), enabled)
	h.recordAudit(r, resp.Action, resp.Target, resp.OK, resp.Message)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleReconnect(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.Reconnect(r.Context())
	h.recordAudit(r, resp.Action, resp.Target, resp.OK, resp.Message)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleMaintenance(w http.ResponseWriter, r *http.Request) {
	var req ControlRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
	}
	resp, err := h.svc.SetMaintenance(r.Context(), req)
	h.recordAudit(r, resp.Action, resp.Target, resp.OK, resp.Message)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleMode(w http.ResponseWriter, r *http.Request) {
	var req ControlRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
	}
	resp, err := h.svc.SetMode(r.Context(), req.Mode)
	h.recordAudit(r, resp.Action, resp.Target, resp.OK, resp.Message)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) recordAudit(r *http.Request, action, target string, ok bool, detail string) {
	if h.audit == nil {
		return
	}
	status := "ok"
	if !ok {
		status = "failed"
	}
	h.audit.Record(r, action, target, status, detail)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
