package server

import (
	"net/http"

	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

// handleReleaseContext serves GET /api/v1/delivery/context (read-only,
// viewer and above). The assembly lives in delivery (release_context.go);
// this only hands it the platform's own read-only readers, looked up at
// request time: release records, approvals, questions and the known
// regressions on main, each read without a sweep or any write.
func (s *Server) handleReleaseContext(w http.ResponseWriter, r *http.Request) {
	src := delivery.ReleaseContextSources{}
	if s.releases != nil && s.releases.Service() != nil {
		src.Current = s.releases.Service().Current
		src.Rule = s.releases.Service().RuleOf
	}
	if s.approvals != nil {
		src.Waiting = s.approvals.Waiting
		src.ApprovalHistory = s.approvals.Observations
	}
	if s.questionRuntime != nil && s.questionRuntime.svc != nil {
		src.Questions = s.questionRuntime.svc.Observations
	}
	src.OpenRegressions = s.openRegressions
	// main is shown, not gated on: read it where the ledger has it.
	src.MainHead = s.mainOf("release-context-main", s.delivery.PolicyFacts().MainHead)
	s.delivery.HandleReleaseContext(src)(w, r)
}
