package flexquery

import (
	"os"
	"strings"
)

const (
	pluginNamespace    = "plugin-flex-query"
	apiDeployName      = "flex-query-api"
	workerDeployName   = "flex-query-worker"
	healthServiceName  = "flex-query-health"
	healthServiceHost  = "flex-query-health.plugin-flex-query.svc.cluster.local"
	healthServicePort  = "8080"
	healthPath         = "/health"
	defaultFreshnessDB = "bifrost_golden_source"
	freshnessMaxAgeH   = 48.0
)

type Config struct {
	HealthURL   string
	FreshnessDB string
	APIBaseURL  string
	WriteToken  string
}

func ConfigFromEnv() Config {
	health := strings.TrimSpace(os.Getenv("FLEX_QUERY_HEALTH_URL"))
	if health == "" {
		health = "http://" + healthServiceHost + ":" + healthServicePort + healthPath
	}
	db := strings.TrimSpace(os.Getenv("FLEX_QUERY_FRESHNESS_DB"))
	if db == "" {
		db = defaultFreshnessDB
	}
	return Config{
		HealthURL:   health,
		FreshnessDB: db,
		APIBaseURL:  strings.TrimRight(strings.TrimSpace(os.Getenv("FLEX_QUERY_API_URL")), "/"),
		WriteToken:  strings.TrimSpace(os.Getenv("FLEX_QUERY_WRITE_TOKEN")),
	}
}
