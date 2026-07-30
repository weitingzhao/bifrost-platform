package marketdata

import "os"

const (
	pluginNamespace   = "plugin-market-data"
	stocksDeployName  = "polygon-worker-stocks"
	optionsDeployName = "polygon-worker-options"
	stocksHealthHost  = "market-data-health-stocks.plugin-market-data.svc.cluster.local"
	optionsHealthHost = "market-data-health-options.plugin-market-data.svc.cluster.local"
	healthServicePort = "8080"
	healthPath        = "/health"
)

// Config holds optional overrides for health probing.
type Config struct {
	StocksHealthURL  string
	OptionsHealthURL string
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
	return Config{StocksHealthURL: stocks, OptionsHealthURL: options}
}
