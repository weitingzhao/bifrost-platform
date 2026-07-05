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
