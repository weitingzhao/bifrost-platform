package telemetry

import "errors"

var (
	ErrPrometheusNotConfigured = errors.New("prometheus url not configured")
	ErrMissingPromQL           = errors.New("missing query parameter q")
)
