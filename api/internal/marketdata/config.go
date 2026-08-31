package marketdata

import (
	"os"
	"strings"
)

const (
	pluginNamespace   = "plugin-market-data"
	stocksDeployName  = "polygon-worker-stocks"
	optionsDeployName = "polygon-worker-options"
	stocksHealthHost  = "market-data-health-stocks.plugin-market-data.svc.cluster.local"
	optionsHealthHost = "market-data-health-options.plugin-market-data.svc.cluster.local"
	healthServicePort = "8080"
	healthPath        = "/health"
	// Golden Source is the only DB that still has data_ops / market.* after
	// write-consolidation DROP on Trade DBs (bifrost_dev / _stg / _prod).
	defaultFreshnessDB = "bifrost_golden_source"
	freshnessMaxAgeH   = 24.0
	// Weekend / Monday-before-EOD allowance for session-bound EOD dimensions.
	freshnessWeekendMaxAgeH = 72.0
)

// Config holds optional overrides for health / freshness probing / Plugin API proxy.
type Config struct {
	StocksHealthURL  string
	OptionsHealthURL string
	FreshnessDB string
	// APIBaseURL overrides Plugin API (:8790) for Console proxy (local port-forward).
	// Example: http://127.0.0.1:8790 — paths are appended as /market/...
	// Also used by readiness_rollup probe (snapshot-coverage / vendor-gap).
	APIBaseURL string
	// WriteToken is MARKET_DATA_WRITE_TOKEN — attached to Plugin mutating
	// proxy calls after Console already passed platform operator auth.
	// Never forwarded to the browser.
	WriteToken string
}

func ConfigFromEnv() Config {
	stocks := os.Getenv("MARKET_DATA_STOCKS_HEALTH_URL")
	if stocks == "" {
		// Backward-compatible override for a single URL (applied to both pools).
		if legacy := os.Getenv("MARKET_DATA_HEALTH_URL"); legacy != "" {
			stocks = legacy
		} else {
			stocks = "http://" + stocksHealthHost + ":" + healthServicePort + healthPath
		}
	}
	options := os.Getenv("MARKET_DATA_OPTIONS_HEALTH_URL")
	if options == "" {
		if legacy := os.Getenv("MARKET_DATA_HEALTH_URL"); legacy != "" {
			options = legacy
		} else {
			options = "http://" + optionsHealthHost + ":" + healthServicePort + healthPath
		}
	}
	db := os.Getenv("MARKET_DATA_FRESHNESS_DB")
	if db == "" {
		db = defaultFreshnessDB
	}
	return Config{
		StocksHealthURL:  stocks,
		OptionsHealthURL: options,
		FreshnessDB:      db,
		APIBaseURL:       strings.TrimRight(strings.TrimSpace(os.Getenv("MARKET_DATA_API_URL")), "/"),
		WriteToken:       strings.TrimSpace(os.Getenv("MARKET_DATA_WRITE_TOKEN")),
	}
}
