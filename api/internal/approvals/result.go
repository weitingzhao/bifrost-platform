package approvals

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/notice"
)

func (s *Service) heartbeat(ref, lease string) decided {
	lease = strings.TrimSpace(lease)
	if lease == "" {
		return decided{Status: 400, Body: map[string]any{"error": "lease_id is required"}}
	}
	doc, err := s.store.read()
	if err != nil {
		return decided{Status: 500, Body: map[string]any{"error": "load approvals: " + err.Error()}}
	}
	found, ok := doc.find(strings.TrimSpace(ref))
	if !ok {
		return decided{Status: 404, Body: map[string]any{"error": "not found"}}
	}
	rec, renewed, err := s.renew(found.ID, lease)
	if err != nil {
		return decided{Status: 500, Body: map[string]any{"error": "store approvals: " + err.Error()}}
	}
	if !renewed {
		msg := "lease mismatch"
		if rec.Status == StatusUnknown {
			msg = "lease lapsed; post the result"
		}
		return decided{Status: 409, Body: map[string]any{"error": msg, "status": rec.Status}}
	}
	return decided{Status: 200, Body: map[string]any{"status": rec.Status, "lease_expires_at": rec.Execution.LeaseExpiresAt}}
}

// resultInput is POST /approvals/{id}/result. Refusal with started=false is a
// transient refusal before anything ran: the record goes back to approved.
type resultInput struct {
	LeaseID      string `json:"lease_id"`
	Started      *bool  `json:"started"`
	Refusal      string `json:"refusal"`
	ExitCode     *int   `json:"exit_code"`
	DurationMS   int64  `json:"duration_ms"`
	OutputSHA256 string `json:"output_sha256"`
	OutputTail   string `json:"output_tail"`
	Error        string `json:"error"`
}

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (s *Service) result(ref string, in resultInput) decided {
	lease := strings.TrimSpace(in.LeaseID)
	if lease == "" {
		return decided{Status: 400, Body: map[string]any{"error": "lease_id is required"}}
	}
	started := in.Started == nil || *in.Started
	refusal := oneLine(Redact(in.Refusal))
	if started && in.ExitCode == nil && strings.TrimSpace(in.Error) == "" {
		return decided{Status: 400, Body: map[string]any{"error": "exit_code or error is required"}}
	}
	if !started && refusal == "" {
		return decided{Status: 400, Body: map[string]any{"error": "refusal is required when started is false"}}
	}
	sum := strings.ToLower(strings.TrimSpace(in.OutputSHA256))
	if sum != "" && !sha256Hex.MatchString(sum) {
		return decided{Status: 400, Body: map[string]any{"error": "output_sha256 must be 64 hex characters"}}
	}
	if in.DurationMS < 0 {
		return decided{Status: 400, Body: map[string]any{"error": "duration_ms must not be negative"}}
	}
	now := s.clock()
	var rec Approval
	var bad *decided
	var evs []event
	err := s.store.update(func(doc *file) error {
		bad, evs = nil, nil
		a, ok := doc.find(strings.TrimSpace(ref))
		if !ok {
			bad = &decided{Status: 404, Body: map[string]any{"error": "not found"}}
			return errNoChange
		}
		e := a.Execution
		if e == nil || e.LeaseID != lease {
			bad = &decided{Status: 409, Body: map[string]any{"error": "lease mismatch", "status": a.Status}}
			return errNoChange
		}
		if lapseRunning(&a, now) {
			markLeaseUnknown(&a)
			evs = append(evs, event{"approval.unknown", a.ID, StatusUnknown, "executor=" + e.ExecutorID})
		}
		late := false
		switch a.Status {
		case StatusRunning:
		case StatusUnknown:
			if e.LateResult {
				bad = &decided{Status: 409, Body: map[string]any{"error": "result already accepted", "status": a.Status}}
				return errNoChange
			}
			late = true
		default:
			bad = &decided{Status: 409, Body: map[string]any{"error": "result already accepted", "status": a.Status}}
			return errNoChange
		}
		if !started {
			if late {
				// The unknown transition is stored with this refusal. Hand the
				// events to the handler so the Owner is notified once. A later
				// request finds the record already unknown and has no events.
				bad = &decided{
					Status: 409,
					Body:   map[string]any{"error": "lease lapsed; a refusal cannot requeue an unknown run", "status": a.Status},
					events: append([]event(nil), evs...),
				}
				doc.put(a)
				rec = a
				return nil
			}
			requeue(&a, refusal, now)
			evs = append(evs, event{"approval.queue", a.ID, StatusApproved, fmt.Sprintf("attempt=%d refusal=%s", e.Attempts, refusal)})
			doc.put(a)
			rec = a
			return nil
		}
		e.FinishedAt = now
		e.DurationMS = in.DurationMS
		if e.DurationMS == 0 && !e.StartedAt.IsZero() {
			e.DurationMS = now.Sub(e.StartedAt).Milliseconds()
		}
		e.ExitCode = in.ExitCode
		e.OutputSHA256 = sum
		e.OutputTail = tail(Redact(in.OutputTail), maxTail)
		e.Error = oneLine(Redact(in.Error))
		e.LeaseExpiresAt = time.Time{}
		e.LateResult = late
		if in.ExitCode != nil && *in.ExitCode == 0 && e.Error == "" {
			a.Status = StatusExecuted
			a.Error = ""
		} else {
			a.Status = StatusFailed
			a.Error = e.Error
			if a.Error == "" {
				a.Error = "exit " + strconv.Itoa(*in.ExitCode)
			}
		}
		name := "approval.run.finish"
		if late {
			name = "approval.late_result"
		}
		evs = append(evs, event{name, a.ID, a.Status, finishDetail(a)})
		doc.put(a)
		rec = a
		return nil
	})
	if err != nil {
		return decided{Status: 500, Body: map[string]any{"error": "store approvals: " + err.Error()}}
	}
	if bad != nil {
		return *bad
	}
	return decided{Status: 200, events: evs, Body: map[string]any{"id": rec.ID, "status": rec.Status, "late_result": rec.Execution.LateResult}}
}

func finishDetail(a Approval) string {
	e := a.Execution
	exit := "none"
	if e.ExitCode != nil {
		exit = strconv.Itoa(*e.ExitCode)
	}
	d := fmt.Sprintf("exit=%s duration_ms=%d executor=%s", exit, e.DurationMS, e.ExecutorID)
	if e.OutputSHA256 != "" {
		d += " output_sha256=" + e.OutputSHA256
	}
	if e.Error != "" {
		d += " error=" + e.Error
	}
	return d
}

// tail keeps the last n bytes on a UTF-8 boundary.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	i := len(s) - n
	for i < len(s) && (s[i]&0xC0) == 0x80 {
		i++
	}
	return s[i:]
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if len(s) > maxErrText {
		s = s[:maxErrText]
	}
	return s
}

// Redact masks common secret shapes before a tail or a clip can cut the
// marker off. The patterns live in notice.
func Redact(s string) string { return notice.Redact(s) }
