package retrospective

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// Handler serves retrospective analysis endpoints.
type Handler struct {
	analyzer *Analyzer
	mu       sync.Mutex
	cache    *AnalysisReport
	cacheAt  time.Time
	cacheTTL time.Duration
}

func NewHandler(analyzer *Analyzer) *Handler {
	return &Handler{
		analyzer: analyzer,
		cacheTTL: 60 * time.Second,
	}
}

// HandleReport returns the full retrospective analysis report.
// GET /api/v1/agent/retrospective/report
func (h *Handler) HandleReport(w http.ResponseWriter, r *http.Request) {
	forceRefresh := r.URL.Query().Get("refresh") == "true"
	report := h.getOrRefresh(forceRefresh)
	writeJSON(w, http.StatusOK, report)
}

// HandlePatterns returns just the pattern clusters.
// GET /api/v1/agent/retrospective/patterns
func (h *Handler) HandlePatterns(w http.ResponseWriter, r *http.Request) {
	report := h.getOrRefresh(false)
	writeJSON(w, http.StatusOK, map[string]any{
		"patterns":     report.Patterns,
		"total":        len(report.Patterns),
		"health_score": report.HealthScore,
		"generated_at": report.GeneratedAt.Format(time.RFC3339),
	})
}

// HandleInsights returns the generated insights.
// GET /api/v1/agent/retrospective/insights
func (h *Handler) HandleInsights(w http.ResponseWriter, r *http.Request) {
	report := h.getOrRefresh(false)
	writeJSON(w, http.StatusOK, map[string]any{
		"insights":     report.Insights,
		"health_score": report.HealthScore,
		"total_jobs":   report.TotalJobs,
		"generated_at": report.GeneratedAt.Format(time.RFC3339),
	})
}

func (h *Handler) getOrRefresh(force bool) AnalysisReport {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !force && h.cache != nil && time.Since(h.cacheAt) < h.cacheTTL {
		return *h.cache
	}
	report := h.analyzer.Analyze()
	h.cache = &report
	h.cacheAt = time.Now()
	return report
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
