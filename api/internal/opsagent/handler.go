package opsagent

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

type Handler struct {
	audit *actuation.AuditLog
	// Announce, when set, writes each alert to the bus (alert.rule.fired) so
	// the seat owning its namespace hears it (2026-11-16: warnings that went
	// only to this webhook reached nobody for 43 hours). A failure is logged;
	// the webhook still answers, and Alertmanager re-sends while firing.
	Announce func(ctx context.Context, alerts []AlertFact) error
}

// AlertFact is what the bus takes of one alert.
type AlertFact struct {
	Name, Namespace, Severity, Status string
}

func NewHandler(audit *actuation.AuditLog) *Handler {
	return &Handler{audit: audit}
}

type alertmanagerAlert struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     string            `json:"startsAt"`
	GeneratorURL string            `json:"generatorURL"`
}

type alertmanagerPayload struct {
	Status            string              `json:"status"`
	Receiver          string              `json:"receiver"`
	GroupLabels       map[string]string   `json:"groupLabels"`
	CommonLabels      map[string]string   `json:"commonLabels"`
	CommonAnnotations map[string]string   `json:"commonAnnotations"`
	Alerts            []alertmanagerAlert `json:"alerts"`
}

type DiagnosisResponse struct {
	OK               bool              `json:"ok"`
	AlertStatus      string            `json:"alert_status"`
	AlertCount       int               `json:"alert_count"`
	Summary          string            `json:"summary"`
	SuggestedL1      []SuggestedAction `json:"suggested_l1"`
	SuggestedL2      []SuggestedAction `json:"suggested_l2"`
	CursorSDKHint    string            `json:"cursor_sdk_hint"`
	MCPPlatformTools []string          `json:"mcp_platform_tools"`
}

type SuggestedAction struct {
	Tool   string `json:"tool"`
	Reason string `json:"reason"`
	Level  string `json:"level"`
}

func (h *Handler) HandleAlertmanager(w http.ResponseWriter, r *http.Request) {
	var payload alertmanagerPayload
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"ok": false, "message": "invalid alertmanager payload: " + err.Error(),
			})
			return
		}
	}
	if len(payload.Alerts) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "message": "no alerts in payload",
		})
		return
	}

	diag := Diagnose(payload)
	principal := actuation.PrincipalFromContext(r.Context())
	detail := diag.Summary
	if len(diag.SuggestedL1) > 0 {
		detail += "; L1: " + diag.SuggestedL1[0].Tool
	}
	if h.audit != nil {
		name := "ops-agent"
		role := principal.Role
		if principal.Name != "" {
			name = principal.Name
		}
		if role == "" {
			role = actuation.RoleReporter
		}
		h.audit.RecordDirect(name, role, "ops-agent.alertmanager", payload.Receiver, "ok", detail)
	}
	if h.Announce != nil {
		facts := make([]AlertFact, 0, len(payload.Alerts))
		for _, a := range payload.Alerts {
			status := a.Status
			if status == "" {
				status = payload.Status
			}
			facts = append(facts, AlertFact{Name: labelVal(a.Labels, "alertname"), Namespace: labelVal(a.Labels, "namespace"),
				Severity: labelVal(a.Labels, "severity"), Status: status})
		}
		if err := h.Announce(r.Context(), facts); err != nil {
			slog.Warn("alerts not announced on the bus", "err", err, "count", len(facts))
		}
	}
	writeJSON(w, http.StatusOK, diag)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func labelVal(labels map[string]string, key string) string {
	if labels == nil {
		return ""
	}
	return labels[key]
}

func alertName(labels map[string]string) string {
	return strings.ToLower(labelVal(labels, "alertname"))
}
