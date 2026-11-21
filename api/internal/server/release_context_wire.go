package server

import (
	"net/http"

	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
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
		src.Waiting = approvalsSeen(s.approvals.Waiting)
		src.ApprovalHistory = approvalsSeen(s.approvals.Observations)
	}
	if s.questionRuntime != nil && s.questionRuntime.Svc != nil {
		src.Questions = answersSeen(s.questionRuntime.Svc.Observations)
	}
	src.OpenRegressions = s.openRegressions
	// main is shown, not gated on: read it where the ledger has it.
	src.MainHead = s.mainOf("release-context-main", s.delivery.PolicyFacts().MainHead)
	s.delivery.HandleReleaseContext(src)(w, r)
}

// approvalsSeen reads the owner's approval records as delivery's release
// context reads them (delivery.ApprovalSeen): which status is a rejection is
// the store's word, said here.
func approvalsSeen(read func() ([]approvals.Approval, error)) func() ([]delivery.ApprovalSeen, error) {
	return func() ([]delivery.ApprovalSeen, error) {
		list, err := read()
		if err != nil {
			return nil, err
		}
		out := make([]delivery.ApprovalSeen, 0, len(list))
		for _, a := range list {
			out = append(out, delivery.ApprovalSeen{
				ID: a.ID, Number: a.Number, Action: a.Action, Requester: a.Requester, Summary: a.Summary,
				CreatedAt: a.CreatedAt, Params: a.Params, KeyParams: a.KeyParams,
				Channel: a.Channel, Rejected: a.Status == approvals.StatusRejected, RejectReason: a.RejectReason,
				DecidedAt: a.DecidedAt,
			})
		}
		return out, nil
	}
}

// answersSeen reads the answered questions as delivery's release context
// reads them (delivery.AnswerSeen): an answer on the rule's channel is a
// rule's, not the Owner's.
func answersSeen(read func() ([]questions.Question, error)) func() ([]delivery.AnswerSeen, error) {
	return func() ([]delivery.AnswerSeen, error) {
		list, err := read()
		if err != nil {
			return nil, err
		}
		out := make([]delivery.AnswerSeen, 0, len(list))
		for _, q := range list {
			if q.Answer == nil {
				continue
			}
			out = append(out, delivery.AnswerSeen{At: q.Answer.At, ByRule: q.Answer.Channel == questions.RuleChannel})
		}
		return out, nil
	}
}
