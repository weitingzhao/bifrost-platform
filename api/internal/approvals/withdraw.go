package approvals

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// A requester may take back its own card (decision 2026-11-15; origin #562,
// #564, #566: a card built wrong or made moot was left for the Owner to
// reject). Withdrawing only ever narrows: it cannot approve or run anything,
// it works only for the requester that built the card, and only before an
// executor has claimed it. The record ends as rejected with a reject_reason
// that starts "withdrawn by requester", so every reader, notice and the
// state machine see the status they already know.

const (
	withdrawnPrefix  = "withdrawn by requester: "
	supersededPrefix = "superseded by "
)

// supersedable: a newer card of the same subject may take the place of a
// card its own requester built, while nobody has decided it or an executor
// has not yet claimed it. Like a withdrawal it only ever narrows.
func supersedable(a Approval, requester string) bool {
	if a.Requester == "" || a.Requester != requester {
		return false
	}
	return a.Status == StatusPending || (a.Status == StatusApproved && !hasBeenClaimed(a))
}

// withdraw closes ref as withdrawn when caller built it and it has not been
// claimed. A card the Owner approved but that no executor has leased yet
// (waiting its turn, or waiting out a refusal) can still be withdrawn.
func (s *Service) withdraw(ref, caller, reason string) decided {
	reason = oneLine(Redact(strings.TrimSpace(reason)))
	if reason == "" {
		return decided{Status: 400, Body: map[string]any{"error": "reason is required"}}
	}
	caller = strings.TrimSpace(caller)
	if caller == "" {
		return decided{Status: 400, Body: map[string]any{"error": "a withdrawal names its requester"}}
	}
	now := s.clock()
	var rec Approval
	var refusal *decided
	var evs []event
	err := s.store.update(func(doc *file) error {
		refusal = nil
		evs = sweep(doc, now)
		found, ok := doc.find(strings.TrimSpace(ref))
		if !ok {
			refusal = &decided{Status: 404, Body: map[string]any{"error": "not found"}}
			return keepSwept(evs)
		}
		if found.Requester == "" || found.Requester != caller {
			refusal = &decided{Status: 403, Body: map[string]any{"error": "only the requester that built this card can withdraw it"}}
			return keepSwept(evs)
		}
		switch {
		case found.Status == StatusExpired:
			refusal = &decided{Status: 409, Body: map[string]any{"error": "expired", "status": StatusExpired}}
		case found.Status == StatusPending:
		case found.Status == StatusApproved && !hasBeenClaimed(found):
		default:
			refusal = &decided{Status: 409, Body: map[string]any{"error": "too late to withdraw: it has run or is running", "status": found.Status}}
		}
		if refusal != nil {
			return keepSwept(evs)
		}
		found.Status = StatusRejected
		found.RejectReason = withdrawnPrefix + reason
		found.DecidedAt = now
		found.DecidedBy = found.Requester
		doc.put(found)
		rec = found
		return nil
	})
	if err != nil {
		return decided{Status: 500, Body: map[string]any{"error": "store approvals: " + err.Error()}}
	}
	s.record(nil, evs)
	if refusal != nil {
		return *refusal
	}
	return decided{Status: 200, Body: map[string]any{"id": rec.ID, "status": rec.Status, "withdrawn": true}}
}

// hasBeenClaimed reports whether an executor ever leased or started the card.
func hasBeenClaimed(a Approval) bool {
	e := a.Execution
	return e != nil && (e.LeaseID != "" || !e.ClaimedAt.IsZero() || !e.StartedAt.IsZero() || !e.FinishedAt.IsZero())
}

// HandleWithdraw is the withdraw route, body {reason}. The requester is the
// caller's session (the header every create stores), never a field of the body.
func (s *Service) HandleWithdraw(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reason string `json:"reason"`
	}
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	out := s.withdraw(chi.URLParam(r, "id"), requester(r), body.Reason)
	if out.Status == http.StatusOK && s.audit != nil {
		id, _ := out.Body["id"].(string)
		s.writeAudit(r, []event{{"approval.withdraw", id, StatusRejected, oneLine(Redact(strings.TrimSpace(body.Reason)))}})
	}
	writeJSON(w, out.Status, out.Body)
}
