package opsagent

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

type Handler struct {
	audit *actuation.AuditLog
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
		h.audit.RecordDirect("ops-agent", actuation.RoleOperator, "ops-agent.alertmanager", payload.Receiver, "ok", detail)
	}
	_ = principal
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
