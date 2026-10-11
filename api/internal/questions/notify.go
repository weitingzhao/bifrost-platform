package questions

import (
	"context"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/notice"
)

func (s *Service) sendCreated(ctx context.Context, c caller, q Question) {
	now := s.clock()
	title, body := NoticeMessage(q, now)
	msg := notice.Message{
		Title:    title,
		Message:  body,
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

// NoticeMessage is the push title and body for one question.
func NoticeMessage(q Question, now time.Time) (string, string) {
	return noticeTitle(q), noticeBody(q, now)
}

func noticeTitle(q Question) string {
	// The title is clipped to 100 runes. Clean before that clip, then clean
	// the composed line. Newlines are removed on a title.
	lead := clipRunes(notice.PushText(q.Title, false), 100)
	title := "Answer · " + numberLabel(q.Number) + " · " + lead
	return notice.PushText(title, false)
}

func noticeBody(q Question, now time.Time) string {
	from := q.Requester
	if q.RequesterThread != "" {
		from = q.RequesterThread
	}
	body := strings.Join([]string{
		notice.PushText(q.Urgency, true),
		"From " + notice.PushText(from, true),
		expiresIn(q.ExpiresAt.Sub(now)),
		"Answering approves nothing.",
	}, "\n")
	return notice.PushText(body, true)
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
