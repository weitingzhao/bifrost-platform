package approvals

import (
	"context"
	"fmt"
	"net/http"

	"github.com/weitingzhao/bifrost-platform/api/internal/approvalnotify"
)

// requestable are the actions the platform itself may request (R5:5.7 goal
// 3): the landing grant cards, asked for when an operation reaches
// awaiting_grant. Every other action is requested by a person or an agent
// through POST /approvals.
var requestable = map[string]bool{"approve_guardrail_landing": true, "approve_core_landing": true}

// RequestError is a request the service refused, with its HTTP status and
// body as POST /approvals would have answered.
type RequestError struct {
	Status int
	Body   map[string]any
}

func (e *RequestError) Error() string {
	if msg, ok := e.Body["error"].(string); ok && msg != "" {
		return fmt.Sprintf("approval request refused (%d): %s", e.Status, msg)
	}
	return fmt.Sprintf("approval request refused (%d)", e.Status)
}

// Request is an in-process request: the same creation as POST /approvals
// (the create check, the one-open-request rule, the PROD home), audited and
// announced to the Owner as a created request is.
func (s *Service) Request(ctx context.Context, requester, action, reason, rollback string, params map[string]any) (Approval, error) {
	if !requestable[action] {
		return Approval{}, &RequestError{Status: http.StatusForbidden, Body: map[string]any{"error": action + " cannot be requested by the platform", "action": action}}
	}
	res := s.createWith(ctx, createInput{Requester: requester, Action: action, Reason: reason, Rollback: rollback, Params: params})
	if res.Status != http.StatusCreated {
		return Approval{}, &RequestError{Status: res.Status, Body: res.Body}
	}
	a := res.Approval
	s.writeAudit(nil, []event{{"approval.create", a.ID, StatusPending,
		fmt.Sprintf("number=%d action=%s tier=%s runner=%s requester=%s drill=%t", a.Number, a.Action, a.Tier, a.Runner, a.Requester, a.Drill)}})
	s.deliver(ctx, approvalnotify.KindCreated, a)
	return a, nil
}
