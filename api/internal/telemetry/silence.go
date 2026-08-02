package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// AttentionMuteRequest — Observability Attention L2 mute (not "fixed").
type AttentionMuteRequest struct {
	AttentionID string            `json:"attention_id"`
	SignalLabel string            `json:"signal_label"`
	Domain      string            `json:"domain"`
	Env         string            `json:"env"`
	Alertname   string            `json:"alertname,omitempty"`
	Matchers    map[string]string `json:"matchers,omitempty"`
	DurationH   int               `json:"duration_hours"`
	Comment     string            `json:"comment,omitempty"`
}

type AttentionMuteResponse struct {
	OK                 bool      `json:"ok"`
	AttentionID        string    `json:"attention_id"`
	ExpiresAt          time.Time `json:"expires_at"`
	Alertmanager       string    `json:"alertmanager"` // created | skipped | error
	AlertmanagerDetail string    `json:"alertmanager_detail,omitempty"`
	SilenceID          string    `json:"silence_id,omitempty"`
	Message            string    `json:"message"`
}

func (s *Service) alertmanagerURL() string {
	if v := strings.TrimSpace(os.Getenv("PLATFORM_ALERTMANAGER_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	// In-cluster default next to kube-prometheus-stack Prometheus.
	prom := s.prometheusURL()
	if strings.Contains(prom, "kube-prometheus-stack-prometheus") {
		return "http://kube-prometheus-stack-alertmanager.monitoring.svc:9093"
	}
	return ""
}

type amSilencePayload struct {
	Matchers  []amMatcher `json:"matchers"`
	StartsAt  time.Time   `json:"startsAt"`
	EndsAt    time.Time   `json:"endsAt"`
	CreatedBy string      `json:"createdBy"`
	Comment   string      `json:"comment"`
}

type amMatcher struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	IsRegex bool   `json:"isRegex"`
	IsEqual bool   `json:"isEqual"`
}

type amSilenceResponse struct {
	SilenceID string `json:"silenceID"`
}

func (s *Service) CreateAttentionMute(ctx context.Context, req AttentionMuteRequest) AttentionMuteResponse {
	hours := req.DurationH
	if hours <= 0 {
		hours = 2
	}
	if hours > 24 {
		hours = 24
	}
	expires := time.Now().UTC().Add(time.Duration(hours) * time.Hour)
	out := AttentionMuteResponse{
		OK:           true,
		AttentionID:  req.AttentionID,
		ExpiresAt:    expires,
		Alertmanager: "skipped",
		Message:      "Observability mute recorded — UI suppress + audit; not a health fix",
	}

	amURL := s.alertmanagerURL()
	alertname := strings.TrimSpace(req.Alertname)
	if alertname == "" {
		alertname = strings.TrimSpace(req.SignalLabel)
	}
	if amURL == "" || alertname == "" {
		out.AlertmanagerDetail = "Alertmanager URL not configured or alertname empty — UI mute only"
		return out
	}

	matchers := []amMatcher{{
		Name: "alertname", Value: alertname, IsEqual: true,
	}}
	for k, v := range req.Matchers {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" || v == "" || k == "alertname" {
			continue
		}
		matchers = append(matchers, amMatcher{Name: k, Value: v, IsEqual: true})
	}

	comment := strings.TrimSpace(req.Comment)
	if comment == "" {
		comment = fmt.Sprintf("Observability Attention mute %dh · %s · %s/%s", hours, req.AttentionID, req.Domain, req.Env)
	}

	payload := amSilencePayload{
		Matchers:  matchers,
		StartsAt:  time.Now().UTC(),
		EndsAt:    expires,
		CreatedBy: "bifrost-ops-console",
		Comment:   comment,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		out.Alertmanager = "error"
		out.AlertmanagerDetail = err.Error()
		return out
	}

	endpoint := amURL + "/api/v2/silences"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		out.Alertmanager = "error"
		out.AlertmanagerDetail = err.Error()
		return out
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: defaultTimeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		out.Alertmanager = "error"
		out.AlertmanagerDetail = err.Error()
		return out
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		out.Alertmanager = "error"
		out.AlertmanagerDetail = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, truncate(string(respBody), 200))
		return out
	}
	var parsed amSilenceResponse
	_ = json.Unmarshal(respBody, &parsed)
	out.Alertmanager = "created"
	out.SilenceID = parsed.SilenceID
	out.AlertmanagerDetail = "Alertmanager silence created"
	out.Message = "Observability mute + Alertmanager silence — still not a root-cause fix"
	return out
}
