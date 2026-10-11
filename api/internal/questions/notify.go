package questions

import (
	"context"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/notice"
)

func (s *Service) sendCreated(ctx context.Context, c caller, q Question) {
	now := s.clock()
	msg := notice.Message{
		Title:    noticeTitle(q),
		Message:  noticeBody(q, now),
		ClickURL: consoleURL(q.ID),
		Priority: 4,
	}
	sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	dels, postErr := s.post(sendCtx, msg)
	stored := clipDeliveries(dels, s.clock())
	var swept []event
	storeErr := s.store.update(func(doc *document) error {
		now := s.clock()
		doc.asOf = now
		swept = sweep(doc, now)
		i, ok := doc.find(q.ID)
		if !ok {
			if len(swept) == 0 {
				return errNoChange
			}
			return nil
		}
		doc.Questions[i].Deliveries = stored
		return nil
	})
	if storeErr != nil {
		s.writeAudit(c, []event{{"question.store", q.ID, "error", "op=notify"}})
		return
	}
	s.writeAudit(c, swept)
	status := "skipped"
	if postErr != nil {
		status = "failed"
	}
	if len(stored) > 0 && stored[0].Result != "" {
		status = stored[0].Result
	}
	s.writeAudit(c, []event{{"question.notify", q.ID, status, ""}})
}

func noticeTitle(q Question) string {
	// The title is clipped to 100 runes. Push the field first, and end the
	// clip before a marker rather than through one. Newlines become spaces.
	lead := clipPushRunes(notice.PushField(q.Title, true), 100)
	return "Answer · " + numberLabel(q.Number) + " · " + lead
}

func noticeBody(q Question, now time.Time) string {
	from := q.Requester
	if q.RequesterThread != "" {
		from = q.RequesterThread
	}
	return strings.Join([]string{
		notice.PushField(q.Urgency, false),
		"From " + notice.PushField(from, false),
		expiresIn(q.ExpiresAt.Sub(now)),
		"Answering approves nothing.",
	}, "\n")
}

// clipPushRunes is clipRunes, except a cut inside a withheld or [redacted …]
// marker ends at the marker's start.
func clipPushRunes(s string, n int) string {
	plain := clipRunes(s, n)
	if plain == s {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	cut := clipEnd(s, len(string(r[:n])))
	return s[:cut]
}

func clipEnd(s string, cut int) int {
	best := cut
	for _, m := range []string{
		"[withheld: hidden characters]",
		"[redacted private key]",
		"[redacted aws key]",
		"[redacted token]",
		"[redacted jwt]",
		"[redacted]",
	} {
		from := 0
		for from < len(s) {
			i := strings.Index(s[from:], m)
			if i < 0 {
				break
			}
			i += from
			end := i + len(m)
			if cut > i && cut < end && i < best {
				best = i
			}
			from = end
		}
	}
	return best
}

func expiresIn(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Hour {
		return "expires in " + itoa(int(d/time.Minute)) + "m"
	}
	return "expires in " + itoa(int(d/time.Hour)) + "h"
}

func clipDeliveries(in []notice.Delivery, now time.Time) []Delivery {
	if len(in) > maxDeliveries {
		in = in[:maxDeliveries]
	}
	out := make([]Delivery, 0, len(in))
	for _, d := range in {
		at := d.At
		if at.IsZero() {
			at = now
		}
		kind := d.Kind
		if kind == "" {
			kind = "created"
		}
		out = append(out, Delivery{
			Kind:    clipEncoded(kind, 32),
			Channel: clipEncoded(d.Channel, 16),
			Target:  clipEncoded(d.Target, 64),
			At:      at,
			Result:  clipEncoded(d.Result, 16),
			Error:   clipEncoded(d.Error, 300),
		})
	}
	return out
}
