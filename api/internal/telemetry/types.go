package telemetry

import "time"

type SamplePoint struct {
	Labels    map[string]string `json:"labels"`
	Value     float64           `json:"value"`
	Timestamp float64           `json:"timestamp,omitempty"`
}

type MetricResult struct {
	ID     string        `json:"id"`
	Title  string        `json:"title"`
	Unit   string        `json:"unit,omitempty"`
	Status string        `json:"status"` // ok | empty | error
	Detail string        `json:"detail,omitempty"`
	Points []SamplePoint `json:"points"`
}

type OverviewResponse struct {
	Namespace     string         `json:"namespace"`
	PrometheusURL string         `json:"prometheus_url,omitempty"`
	LayerBStatus  string         `json:"layer_b_status,omitempty"`
	Reachability  string         `json:"reachability,omitempty"`
	Metrics       []MetricResult `json:"metrics"`
	GeneratedAt   time.Time      `json:"generated_at"`
}

type QueryResponse struct {
	Namespace string       `json:"namespace"`
	QueryID   string       `json:"query_id"`
	Metric    MetricResult `json:"metric"`
}

type PromQLResponse struct {
	PrometheusURL string        `json:"prometheus_url,omitempty"`
	Query         string        `json:"query"`
	ResultType    string        `json:"result_type,omitempty"`
	Points        []SamplePoint `json:"points"`
	GeneratedAt   time.Time     `json:"generated_at"`
}

type AlertEntry struct {
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	State       string            `json:"state"`
	ActiveAt    string            `json:"active_at,omitempty"`
	Value       string            `json:"value,omitempty"`
}

type AlertsResponse struct {
	PrometheusURL string       `json:"prometheus_url,omitempty"`
	Alerts        []AlertEntry `json:"alerts"`
	GeneratedAt   time.Time    `json:"generated_at"`
}

type TargetEntry struct {
	Labels           map[string]string `json:"labels"`
	ScrapePool       string            `json:"scrape_pool,omitempty"`
	ScrapeURL        string            `json:"scrape_url,omitempty"`
	Health           string            `json:"health"`
	LastError        string            `json:"last_error,omitempty"`
	LastScrape       string            `json:"last_scrape,omitempty"`
	LastScrapeDuration float64         `json:"last_scrape_duration,omitempty"`
}

type TargetsResponse struct {
	PrometheusURL string        `json:"prometheus_url,omitempty"`
	ActiveTargets []TargetEntry `json:"active_targets"`
	DroppedTargets []TargetEntry `json:"dropped_targets,omitempty"`
	GeneratedAt   time.Time     `json:"generated_at"`
}
