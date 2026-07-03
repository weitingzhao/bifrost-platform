package network

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

type Handler struct {
	svc *Service
}

func NewHandler(opts ...ServiceOption) *Handler {
	return &Handler{svc: NewService(opts...)}
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

func (h *Handler) HandleAudit(w http.ResponseWriter, r *http.Request) {
	h.writeService(w, r, h.svc.Audit)
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
