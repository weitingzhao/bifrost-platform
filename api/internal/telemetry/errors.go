package telemetry

import "errors"

var (
	ErrPrometheusNotConfigured = errors.New("prometheus url not configured")
	ErrUnknownQuery            = errors.New("unknown telemetry query id")
	ErrMissingPromQL           = errors.New("missing query parameter q")
)
