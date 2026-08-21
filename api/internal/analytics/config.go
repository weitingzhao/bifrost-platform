package analytics

import (
	"os"
	"strings"
)

const (
	pluginNamespace = "plugin-market-data"
	docsServiceName = "analytics-docs"
	docsServicePort = "8061"
	cronJobName     = "bifrost-analytics-daily"
	docsDeployName  = "analytics-docs"
	proxyTimeoutSec = 60
)

type Config struct {
	// DocsBaseURL overrides in-cluster proxy (e.g. http://127.0.0.1:8061 for local).
	DocsBaseURL string
}

func ConfigFromEnv() Config {
	return Config{
		DocsBaseURL: strings.TrimRight(strings.TrimSpace(os.Getenv("ANALYTICS_DOCS_URL")), "/"),
	}
}
