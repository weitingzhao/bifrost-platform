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
	var kp map[string]string
	if len(a.KeyParams) > 0 {
		kp = make(map[string]string, len(a.KeyParams))
		for k, v := range a.KeyParams {
			kp[k] = Redact(v)
		}
	}
	return approvalnotify.Item{
		ID:        a.ID,
		Number:    a.Number,
		Action:    a.Action,
		Tier:      a.Tier,
		Env:       Redact(a.Env),
		Summary:   Redact(a.Summary),
		KeyParams: kp,
		Requester: Redact(a.Requester),
		Thread:    Redact(a.RequesterThread),
		WorkID:    Redact(a.WorkID),
		Runner:    a.Runner,
		CreatedAt: a.CreatedAt,
		ExpiresAt: a.ExpiresAt,
		Error:     Redact(oneLine(a.Error)),
	}
}

// deliver sends one notice about rec and stores every delivery on it, failed
// ones included, with one approval.notify audit line each.
func (s *Service) deliver(ctx context.Context, kind string, rec Approval) {
	ds := noticeDeliveries(approvalnotify.Send(ctx, kind, noticeItem(rec), s.clock()))
	s.record(nil, noticeAudit(rec.ID, ds))
	if err := s.RecordDeliveries(rec.ID, ds); err != nil {
		s.record(nil, []event{{"approval.notify", rec.ID, approvalnotify.ResultFailed, "store deliveries: " + err.Error()}})
	}
}

const (
	// relayClaimTarget is the claim's target. The relay's ntfy hash is not
	// known until the send returns.
	relayClaimTarget = "relay"
	deliveryClaimed  = "claimed"
	maxDeliveries    = 20
	// reminderClaimFor is how long a claim blocks another worker. It outlives
	// one send. After it, a crashed worker's claim can be taken again.
	reminderClaimFor = 2 * noticeTimeout
)

// deliverReminder claims (approval, kind, target "relay") inside the state
// write before sending. A send whose deliveries all failed releases the claim
// so the next pass retries. A skipped send (no relay configured) is kept, so
// a missing relay does not spin. Two workers racing on one store send once.
func (s *Service) deliverReminder(ctx context.Context, kind string, rec Approval) {
	now := s.clock()
	claimID, ok, err := s.claimReminder(rec.ID, kind, now)
	if err != nil || !ok {
		return
	}
	ds := noticeDeliveries(approvalnotify.Send(ctx, kind, noticeItem(rec), now))
	evs := noticeAudit(rec.ID, ds)
	if allFailed(ds) {
		if err := s.releaseReminder(rec.ID, kind, claimID); err != nil {
			evs = append(evs, event{"approval.notify", rec.ID, approvalnotify.ResultFailed, "release reminder: " + err.Error()})
		}
		s.record(nil, evs)
		return
	}
	if err := s.finishReminder(rec.ID, kind, claimID, ds); err != nil {
		s.record(nil, []event{{"approval.notify", rec.ID, approvalnotify.ResultFailed, "store deliveries: " + err.Error()}})
		return
	}
	s.record(nil, evs)
}

func (s *Service) claimReminder(id, kind string, now time.Time) (string, bool, error) {
	claimID := "claim_" + randomHex(8)
	expires := now.Add(reminderClaimFor)
	claimed := false
	err := s.store.update(func(doc *file) error {
		claimed = false
		a, ok := doc.find(id)
		if !ok || reminderDue(a, now) != kind {
			return errNoChange
		}
		if liveClaim(a, kind, now) {
			return errNoChange
		}
		a.Deliveries = capDeliveries(append(dropExpiredClaims(a.Deliveries, kind, now), Delivery{
			Kind: kind, Channel: "ntfy", Target: relayClaimTarget, At: now, Result: deliveryClaimed,
			ClaimID: claimID, ClaimExpiresAt: expires,
		}))
		doc.put(a)
		claimed = true
		return nil
	})
	if err != nil || !claimed {
		return "", false, err
	}
	return claimID, true, nil
}

func (s *Service) releaseReminder(id, kind, claimID string) error {
	if claimID == "" {
		return fmt.Errorf("reminder claim id is required")
	}
	return s.store.update(func(doc *file) error {
		a, ok := doc.find(id)
		if !ok {
			return fmt.Errorf("approval %s not found", id)
		}
		if !hasClaim(a.Deliveries, kind, claimID) {
			return errNoChange
		}
		a.Deliveries = withoutClaim(a.Deliveries, kind, claimID)
		doc.put(a)
		return nil
	})
}

func (s *Service) finishReminder(id, kind, claimID string, ds []Delivery) error {
	if claimID == "" {
		return fmt.Errorf("reminder claim id is required")
	}
	return s.store.update(func(doc *file) error {
		a, ok := doc.find(id)
		if !ok {
			return fmt.Errorf("approval %s not found", id)
		}
		if !hasClaim(a.Deliveries, kind, claimID) {
			return fmt.Errorf("reminder claim %s does not match", claimID)
		}
		a.Deliveries = capDeliveries(append(withoutClaim(a.Deliveries, kind, claimID), ds...))
		doc.put(a)
		return nil
	})
}

func noticeDeliveries(ds []approvalnotify.Delivery) []Delivery {
	out := make([]Delivery, 0, len(ds))
	for _, d := range ds {
		out = append(out, Delivery{Kind: d.Kind, Channel: d.Channel, Target: d.Target, At: d.At, Result: d.Result, Error: d.Error})
	}
	return out
}

func noticeAudit(id string, ds []Delivery) []event {
	evs := make([]event, 0, len(ds))
	for _, d := range ds {
		detail := fmt.Sprintf("kind=%s channel=%s target=%s", d.Kind, d.Channel, d.Target)
		if d.Error != "" {
			detail += " error=" + d.Error
		}
		evs = append(evs, event{"approval.notify", id, d.Result, detail})
	}
	return evs
}

func allFailed(ds []Delivery) bool {
	if len(ds) == 0 {
		return true
	}
	for _, d := range ds {
		if d.Result != approvalnotify.ResultFailed {
			return false
		}
	}
	return true
}

func withoutClaim(ds []Delivery, kind, claimID string) []Delivery {
	out := make([]Delivery, 0, len(ds))
	for _, d := range ds {
		if d.Kind == kind && d.Result == deliveryClaimed && d.Target == relayClaimTarget && d.ClaimID == claimID {
			continue
		}
		out = append(out, d)
	}
	return out
}

func hasClaim(ds []Delivery, kind, claimID string) bool {
	for _, d := range ds {
		if d.Kind == kind && d.Result == deliveryClaimed && d.ClaimID == claimID {
			return true
		}
	}
	return false
}

func liveClaim(a Approval, kind string, now time.Time) bool {
	for _, d := range a.Deliveries {
		if d.Kind == kind && d.Result == deliveryClaimed && d.ClaimExpiresAt.After(now) {
			return true
		}
	}
	return false
}

func dropExpiredClaims(ds []Delivery, kind string, now time.Time) []Delivery {
	out := make([]Delivery, 0, len(ds))
	for _, d := range ds {
		if d.Kind == kind && d.Result == deliveryClaimed && !d.ClaimExpiresAt.After(now) {
			continue
		}
		out = append(out, d)
	}
	return out
}

// capDeliveries keeps at most maxDeliveries, dropping the oldest records that
// are not the latest final result (failed, unknown, or not_executed).
func capDeliveries(ds []Delivery) []Delivery {
	if len(ds) <= maxDeliveries {
		return ds
	}
	keep := -1
	for i := len(ds) - 1; i >= 0; i-- {
		if isFinalResultKind(ds[i].Kind) {
			keep = i
			break
		}
	}
	out := append([]Delivery(nil), ds...)
	for len(out) > maxDeliveries {
		drop := 0
		if keep == 0 {
			drop = 1
		}
		out = append(out[:drop], out[drop+1:]...)
		if keep > drop {
			keep--
		}
	}
	return out
}

func isFinalResultKind(kind string) bool {
	switch kind {
	case approvalnotify.KindFailed, approvalnotify.KindUnknown, approvalnotify.KindNotExecuted:
		return true
	default:
		return false
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
			s.deliverReminder(ctx, kind, a)
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
		if !reminderHeld(a, approvalnotify.KindExpiring, now) {
			return approvalnotify.KindExpiring
		}
	case now.Sub(a.CreatedAt) >= approvalnotify.RemindAfter:
		if !reminderHeld(a, approvalnotify.KindWaiting, now) {
			return approvalnotify.KindWaiting
		}
	}
	return ""
}

// reminderHeld is true when this kind was already sent, or a claim that has
// not expired still owns it. An expired claim does not hold the reminder.
func reminderHeld(a Approval, kind string, now time.Time) bool {
	for _, d := range a.Deliveries {
		if d.Kind != kind {
			continue
		}
		if d.Result == deliveryClaimed {
			if d.ClaimExpiresAt.After(now) {
				return true
			}
			continue
		}
		return true
	}
	return false
}
