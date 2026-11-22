package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

type Handler struct {
	cfg   *config.Config
	svc   *Service
	audit *actuation.AuditLog
}

func NewHandler(cfg *config.Config, audit *actuation.AuditLog) *Handler {
	return &Handler{
		cfg:   cfg,
		svc:   NewService(cfg),
		audit: audit,
	}
}

// SetGroup registers a domain's group of readings (Service.SetGroup).
func (h *Handler) SetGroup(name string, queries func() []QuerySpec) { h.svc.SetGroup(name, queries) }

func (h *Handler) HandleOverview(w http.ResponseWriter, r *http.Request) {
	ns := r.URL.Query().Get("ns")
	resp, err := h.svc.Overview(r.Context(), ns, r.URL.Query().Get("group"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandlePromQL(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	resp, err := h.svc.PromQL(r.Context(), q)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleAlerts(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.Alerts(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleTargets(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	resp, err := h.svc.Targets(r.Context(), state)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// HandleAttentionMute — L2 mute for Observability Attention (operator).
// Audits always; best-effort Alertmanager silence when configured.
// Explicitly NOT a health fix — UI suppress + optional AM silence only.
func (h *Handler) HandleAttentionMute(w http.ResponseWriter, r *http.Request) {
	var req AttentionMuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if req.AttentionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "attention_id required"})
		return
	}
	resp := h.svc.CreateAttentionMute(r.Context(), req)
	status := "ok"
	if resp.Alertmanager == "error" {
		status = "partial"
	}
	detail := fmt.Sprintf(
		"signal=%s domain=%s env=%s am=%s silence=%s expires=%s",
		req.SignalLabel, req.Domain, req.Env, resp.Alertmanager, resp.SilenceID, resp.ExpiresAt.Format(time.RFC3339),
	)
	if h.audit != nil {
		h.audit.Record(r, "telemetry.attention_mute", req.AttentionID, status, detail)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrPrometheusNotConfigured):
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": err.Error(),
			"hint":  "Set observability_urls.prometheus in clusters.yaml or PLATFORM_PROMETHEUS_URL",
		})
	case errors.Is(err, ErrMissingPromQL):
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": err.Error(),
		})
	default:
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": err.Error(),
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ScrapedHosts exposes the scraped host inventory to other packages (topology).
func (h *Handler) ScrapedHosts(ctx context.Context) map[string]string {
	return h.svc.ScrapedHosts(ctx)
}

// RuleGroups is the rule groups Prometheus has loaded, for the map; false
// when Prometheus is not configured or did not answer.
func (h *Handler) RuleGroups(ctx context.Context) ([]RuleGroup, bool) {
	promURL := h.svc.prometheusURL()
	if promURL == "" {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	groups, err := NewClient(promURL).QueryRuleGroups(ctx)
	return groups, err == nil
}

// PromQL runs one instant query for another package (the bus's effect
// readings); an empty result is no points, not an error.
func (h *Handler) PromQL(ctx context.Context, promQL string) ([]SamplePoint, error) {
	resp, err := h.svc.PromQL(ctx, promQL)
	return resp.Points, err
}

// PromQLSum answers a query as one number per series key: the first of labels a
// series has a value for ("" when none), series of one key added up.
func (h *Handler) PromQLSum(ctx context.Context, promQL string, labels []string) (map[string]float64, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	points, err := h.PromQL(ctx, promQL)
	if err != nil {
		return nil, err
	}
	out := map[string]float64{}
	for _, p := range points {
		key := ""
		for _, k := range labels {
			if v := p.Labels[k]; v != "" {
				key = v
				break
			}
		}
		out[key] += p.Value
	}
	return out, nil
}
