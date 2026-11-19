package server

import (
	"context"

	"github.com/weitingzhao/bifrost-platform/api/internal/cirunview"
)

// startRunSummaries writes each finished run's task summary onto the run
// (cirunview.StartSummaries).
func (s *Server) startRunSummaries(ctx context.Context) {
	if s.delivery == nil {
		return
	}
	cirunview.StartSummaries(ctx, &s.bus.runHooks, s.delivery.PipelinesNamespace(), s.clusterDynamic)
}
