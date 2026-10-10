package approvals

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/approvalnotify"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// noticeTimeout bounds one notice sent off the request path.
const noticeTimeout = 30 * time.Second

func noticeItem(a Approval) approvalnotify.Item {
	return approvalnotify.Item{
		ID:        a.ID,
		Number:    a.Number,
		Action:    a.Action,
		Tier:      a.Tier,
		Env:       a.Env,
		Summary:   a.Summary,
		KeyParams: a.KeyParams,
		Requester: a.Requester,
		Thread:    a.RequesterThread,
		WorkID:    a.WorkID,
		Runner:    a.Runner,
		CreatedAt: a.CreatedAt,
		ExpiresAt: a.ExpiresAt,
		Error:     Redact(oneLine(a.Error)),
	}
}

// deliver sends one notice about rec and stores every delivery on it, failed
// ones included, with one approval.notify audit line each.
func (s *Service) deliver(ctx context.Context, kind string, rec Approval) {
	ds := approvalnotify.Send(ctx, kind, noticeItem(rec), s.clock())
	out := make([]Delivery, 0, len(ds))
	evs := make([]event, 0, len(ds))
	for _, d := range ds {
		out = append(out, Delivery{Kind: d.Kind, Channel: d.Channel, Target: d.Target, At: d.At, Result: d.Result, Error: d.Error})
		detail := fmt.Sprintf("kind=%s channel=%s target=%s", d.Kind, d.Channel, d.Target)
		if d.Error != "" {
			detail += " error=" + d.Error
		}
		evs = append(evs, event{"approval.notify", rec.ID, d.Result, detail})
	}
	s.record(nil, evs)
	if err := s.RecordDeliveries(rec.ID, out); err != nil {
		s.record(nil, []event{{"approval.notify", rec.ID, approvalnotify.ResultFailed, "store deliveries: " + err.Error()}})
	}
}

// noticeKind is the Owner notice an audit event calls for, or "". Success is
// not pushed (ADR §6: only what needs the Owner).
func noticeKind(e event) string {
	switch e.action {
	case "approval.execute", "approval.run.finish", "approval.late_result":
		if e.status == StatusFailed {
			return approvalnotify.KindFailed
		}
	case "approval.unknown":
		return approvalnotify.KindUnknown
	case "approval.expire":
		if strings.HasPrefix(e.detail, "execution deadline") {
			return approvalnotify.KindNotExecuted
		}
	}
	return ""
}

// noticeEvents pushes the transitions in evs that need the Owner. With a relay
// configured the send runs in the background, so a sweep inside a GET or a
// claim does not wait on the phone.
func (s *Service) noticeEvents(evs []event) {
	for _, e := range evs {
		kind := noticeKind(e)
		if kind == "" {
			continue
		}
		rec, ok := s.find(e.target)
		if !ok {
			continue
		}
		if !approvalnotify.Configured() {
			s.deliver(context.Background(), kind, rec)
			continue
		}
		s.notices.Add(1)
		safego.Go("approvals.notify", func() {
			defer s.notices.Done()
			ctx, cancel := context.WithTimeout(context.Background(), noticeTimeout)
			defer cancel()
			s.deliver(ctx, kind, rec)
		})
	}
}

// find reads one record without sweeping.
func (s *Service) find(ref string) (Approval, bool) {
	doc, err := s.store.read()
	if err != nil {
		return Approval{}, false
	}
	return doc.find(ref)
}

// remindDue sends the two reminders for a pending record, each at most once:
// waiting RemindAfter since it was created, and RemindBeforeExpiry before it
// expires. When both are due only the expiry one is sent.
func (s *Service) remindDue(ctx context.Context) {
	doc, err := s.store.read()
	if err != nil {
		return
	}
	now := s.clock()
	for _, a := range doc.Approvals {
		if kind := reminderDue(a, now); kind != "" {
			s.deliver(ctx, kind, a)
		}
	}
}

func reminderDue(a Approval, now time.Time) string {
	if a.Status != StatusPending || !now.Before(a.ExpiresAt) {
		return ""
	}
	left := a.ExpiresAt.Sub(now)
	switch {
	case left <= approvalnotify.RemindBeforeExpiry:
		if !a.notified(approvalnotify.KindExpiring) {
			return approvalnotify.KindExpiring
		}
	case now.Sub(a.CreatedAt) >= approvalnotify.RemindAfter:
		if !a.notified(approvalnotify.KindWaiting) {
			return approvalnotify.KindWaiting
		}
	}
	return ""
}

func (a Approval) notified(kind string) bool {
	for _, d := range a.Deliveries {
		if d.Kind == kind {
			return true
		}
	}
	return false
}
