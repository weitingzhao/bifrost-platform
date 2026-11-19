package server

import (
	"context"
	"fmt"

	"github.com/weitingzhao/bifrost-platform/api/internal/placement"
)

// buildNodeMetrics hands build placement the node metrics (placement.PromNodeMetrics
// reads them) from this platform's Prometheus reader.
func (s *Server) buildNodeMetrics(ctx context.Context) (map[string]placement.NodeMetrics, error) {
	if s.telemetry == nil {
		return nil, fmt.Errorf("no Prometheus reader on this platform")
	}
	return s.nodeLatency.Read(ctx, s.telemetry.PromQL)
}
