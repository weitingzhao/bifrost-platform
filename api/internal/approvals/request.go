package approvals

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/approvalnotify"
)

// requestable are the actions the platform itself may request (R5:5.7 goal
// 3): the landing grant cards, asked for when an operation reaches
// awaiting_grant, and deploy_agentd, asked for when an execution host runs
// another agentd build than PROD's platform commit. Every other action is
// requested by a person or an agent through POST /approvals.
var requestable = map[string]bool{"deploy_agentd": true}

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

// Withdraw takes back a pending request the platform may make (requestable)
// that can no longer run, off the Owner's list: a landing grant's operation
// landed, or ended, without it. It becomes expired, with reason; one already expired is left
// so. A request already decided otherwise, or one of any other action, is
// refused and left as it is.
func (s *Service) Withdraw(ref, reason string) error {
	reason = oneLine(Redact(strings.TrimSpace(reason)))
	if reason == "" {
		return &RequestError{Status: http.StatusBadRequest, Body: map[string]any{"error": "reason is required"}}
	}
	now := s.clock()
	var refusal *RequestError
	var evs []event
	err := s.store.update(func(doc *file) error {
		refusal = nil
		evs = sweep(doc, now)
		found, ok := doc.find(strings.TrimSpace(ref))
		switch {
		case !ok:
			refusal = &RequestError{Status: http.StatusNotFound, Body: map[string]any{"error": "not found"}}
		case !requestable[found.Action]:
			refusal = &RequestError{Status: http.StatusForbidden, Body: map[string]any{"error": found.Action + " cannot be withdrawn by the platform"}}
		case found.Status == StatusExpired:
			return keepSwept(evs)
		case found.Status != StatusPending:
			refusal = &RequestError{Status: http.StatusConflict, Body: map[string]any{"error": "already decided", "status": found.Status}}
		}
		if refusal != nil {
			return keepSwept(evs)
		}
		found.Status = StatusExpired
		found.Error = "withdrawn: " + reason
		found.DecidedAt = now
		doc.put(found)
		evs = append(evs, event{"approval.withdraw", found.ID, StatusExpired, reason})
		return nil
	})
	if err != nil {
		return fmt.Errorf("store approvals: %w", err)
	}
	s.record(nil, evs)
	if refusal != nil {
		return refusal
	}
	return nil
}
