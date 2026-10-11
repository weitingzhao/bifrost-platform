package approvals

import (
	"strings"
	"time"
)

// resolveNoteMax is the longest note the Owner may attach when closing an
// unknown result. Longer than a reject reason, and rejected rather than clipped.
const resolveNoteMax = 500

// resolve closes an unknown approval. succeeded becomes executed; failed and
// not_run become failed. The lease is cleared so a later result or heartbeat
// with the old lease is a mismatch and changes nothing. No notice is sent:
// the handler audits approval.resolve and does not call record.
func (s *Service) resolve(ref, outcome, note string) decided {
	outcome = strings.TrimSpace(outcome)
	switch outcome {
	case "not_run", "succeeded", "failed":
	default:
		return decided{Status: 400, Body: map[string]any{"error": "outcome must be not_run, succeeded, or failed"}}
	}
	cleaned, errMsg := cleanResolveNote(note)
	if errMsg != "" {
		return decided{Status: 400, Body: map[string]any{"error": errMsg}}
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
		if found.Status != StatusUnknown {
			refusal = &decided{Status: 409, Body: map[string]any{
				"error":  "only an unknown result can be closed",
				"status": found.Status,
			}}
			return keepSwept(evs)
		}
		switch outcome {
		case "succeeded":
			found.Status = StatusExecuted
			found.Error = ""
		case "failed":
			found.Status = StatusFailed
		default:
			found.Status = StatusFailed
			found.Error = "did not run (closed by the Owner)"
		}
		found.Resolution = &Resolution{Outcome: outcome, Note: cleaned, At: now, By: "owner"}
		if found.Execution != nil {
			found.Execution.LeaseID = ""
			found.Execution.LeaseExpiresAt = time.Time{}
		}
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
	return decided{Status: 200, Body: map[string]any{
		"id":         rec.ID,
		"status":     rec.Status,
		"outcome":    outcome,
		"note":       cleaned,
		"resolution": rec.Resolution,
	}}
}

// cleanResolveNote redacts like a reject reason (one line, secrets masked)
// and refuses an empty or oversized note.
func cleanResolveNote(note string) (string, string) {
	note = strings.TrimSpace(note)
	if note == "" {
		return "", "note is required"
	}
	if len([]rune(note)) > resolveNoteMax {
		return "", "note is at most 500 characters"
	}
	note = Redact(note)
	note = strings.TrimSpace(note)
	if i := strings.IndexAny(note, "\r\n"); i >= 0 {
		note = strings.TrimSpace(note[:i])
	}
	if note == "" {
		return "", "note is required"
	}
	return note, ""
}
