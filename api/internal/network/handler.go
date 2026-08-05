package network

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

type Handler struct {
	svc   *Service
	audit *actuation.AuditLog
}

func NewHandler(audit *actuation.AuditLog, opts ...ServiceOption) *Handler {
	return &Handler{svc: NewService(opts...), audit: audit}
}

func (h *Handler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	h.writeService(w, r, h.svc.Status)
}

func (h *Handler) HandleZones(w http.ResponseWriter, r *http.Request) {
	h.writeService(w, r, h.svc.Zones)
}

func (h *Handler) HandlePolicies(w http.ResponseWriter, r *http.Request) {
	h.writeService(w, r, h.svc.Policies)
}

func (h *Handler) HandleDevices(w http.ResponseWriter, r *http.Request) {
	h.writeService(w, r, h.svc.Devices)
}

func (h *Handler) HandleClients(w http.ResponseWriter, r *http.Request) {
	h.writeService(w, r, h.svc.Clients)
}

func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	h.writeService(w, r, h.svc.Health)
}

func (h *Handler) HandleBandwidth(w http.ResponseWriter, r *http.Request) {
	h.writeService(w, r, h.svc.Bandwidth)
}

func (h *Handler) HandleAnomalies(w http.ResponseWriter, r *http.Request) {
	h.writeService(w, r, h.svc.Anomalies)
}

func (h *Handler) HandleSLA(w http.ResponseWriter, r *http.Request) {
	h.writeService(w, r, h.svc.SLA)
}

func (h *Handler) HandleAudit(w http.ResponseWriter, r *http.Request) {
	h.writeService(w, r, h.svc.Audit)
}

func (h *Handler) HandleFirewallApply(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IncludeDefaultDeny bool `json:"include_default_deny"`
	}
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
	}

	target := "/api/v1/network/firewall/apply"
	action := "network.firewall.apply"

	result, err := h.svc.ApplyFirewall(r.Context(), req.IncludeDefaultDeny)
	auditStatus := "ok"
	detail := "firewall apply completed"
	if err != nil {
		auditStatus = "failed"
		detail = err.Error()
	}
	if h.audit != nil {
		h.audit.Record(r, action, target, auditStatus, detail)
	}
	if err != nil {
		h.writeError(w, err)
		return
	}

	payload := map[string]any{
		"action":               action,
		"target":               target,
		"autonomy":             "L1",
		"include_default_deny": req.IncludeDefaultDeny,
		"result":               result,
		"message":              detail,
	}
	if postAudit, auditErr := h.svc.Audit(r.Context()); auditErr == nil {
		payload["post_apply_audit"] = postAudit
	}

	writeJSON(w, http.StatusOK, payload)
}

func (h *Handler) writeService(w http.ResponseWriter, r *http.Request, fn func(context.Context) (map[string]any, error)) {
	payload, err := fn(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (h *Handler) writeError(w http.ResponseWriter, err error) {
	msg := err.Error()
	status := http.StatusBadGateway
	if strings.Contains(msg, "UNIFI_USER") || strings.Contains(msg, "required") {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{
		"error":   msg,
		"hint":    "Set UNIFI_HOST, UNIFI_USER, UNIFI_PASS for bifrost-agent Session v2",
		"autonomy": "L0",
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
