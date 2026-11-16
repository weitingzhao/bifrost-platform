package server

import "github.com/weitingzhao/bifrost-platform/api/internal/httpmetrics"

// HTTP observations belong to the shared foundation; server only wires them.
type httpMetrics = httpmetrics.Metrics

func newHTTPMetrics() *httpMetrics { return httpmetrics.New() }
