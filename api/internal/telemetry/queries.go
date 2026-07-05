package telemetry

import "strings"

type QuerySpec struct {
	ID    string
	Title string
	Unit  string
	Build func(ns string) string
}

func PresetQueries() []QuerySpec {
	return []QuerySpec{
		{
			ID:    "api_request_rate",
			Title: "API request rate",
			Unit:  "req/s",
			Build: func(ns string) string {
				return `sum(rate(http_requests_total{namespace="` + ns + `"}[5m])) by (service)`
			},
		},
		{
			ID:    "api_latency_p99",
			Title: "API latency P99",
			Unit:  "s",
			Build: func(ns string) string {
				return `histogram_quantile(0.99, sum(rate(http_request_duration_seconds_bucket{namespace="` + ns + `"}[5m])) by (le, service))`
			},
		},
		{
			ID:    "api_error_rate",
			Title: "API 5xx error rate",
			Unit:  "ratio",
			Build: func(ns string) string {
				return `sum(rate(http_requests_total{namespace="` + ns + `",status="5xx"}[5m])) by (service) / sum(rate(http_requests_total{namespace="` + ns + `"}[5m])) by (service)`
			},
		},
		{
			ID:    "redis_memory_bytes",
			Title: "Redis memory used",
			Unit:  "bytes",
			Build: func(_ string) string {
				return `redis_memory_used_bytes{namespace="data"}`
			},
		},
		{
			ID:    "redis_connected_clients",
			Title: "Redis connected clients",
			Unit:  "clients",
			Build: func(_ string) string {
				return `redis_connected_clients{namespace="data"}`
			},
		},
		{
			ID:    "pg_connections",
			Title: "PostgreSQL connections",
			Unit:  "connections",
			Build: func(_ string) string {
				return `cnpg_collector_pg_stat_activity_count{namespace="data"}`
			},
		},
		{
			ID:    "pg_replication_lag",
			Title: "PostgreSQL replication lag",
			Unit:  "s",
			Build: func(_ string) string {
				return `cnpg_collector_pg_replication_lag{namespace="data"}`
			},
		},
	}
}

func FindQuery(id string) (QuerySpec, bool) {
	id = strings.TrimSpace(id)
	for _, q := range PresetQueries() {
		if q.ID == id {
			return q, true
		}
	}
	return QuerySpec{}, false
}

func DefaultNamespace() string {
	return "bifrost-stg"
}

func ResolveNamespace(raw string) string {
	ns := strings.TrimSpace(raw)
	if ns == "" {
		return DefaultNamespace()
	}
	return ns
}
