package server

import (
	"context"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

type approvalLoops interface {
	RunRetries(context.Context, time.Duration)
	RunHeldFinishes(context.Context, time.Duration)
}

// Exactly one loop owns held-result flushing in each process, including all.
func runApprovalLoop(ctx context.Context, svc approvalLoops, workers bool, every time.Duration) {
	if workers {
		svc.RunRetries(ctx, every)
		return
	}
	svc.RunHeldFinishes(ctx, every)
}

func (s *Server) startApprovalLoop(workers bool) {
	ctx, cancel := context.WithCancel(context.Background())
	s.approvalsCancel = cancel
	safego.Go("approvals.results", func() {
		runApprovalLoop(ctx, s.approvals, workers, approvalRetryInterval)
	})
}
