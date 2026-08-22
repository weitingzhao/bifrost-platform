package research

import (
	"os"
	"strings"
)

const (
	pluginNamespace = "research"
	apiServiceName  = "research-api"
	apiServicePort  = "8795"
	healthPath      = "/health"
)

// Config holds optional overrides for Research API proxy / health probe.
type Config struct {
	// APIBaseURL overrides in-cluster kube service proxy (local port-forward).
	// Example: http://127.0.0.1:8795 — paths are appended as /health, /analytics/...
	APIBaseURL string
}

func ConfigFromEnv() Config {
	return Config{
		APIBaseURL: strings.TrimRight(strings.TrimSpace(os.Getenv("RESEARCH_API_URL")), "/"),
	}
}
