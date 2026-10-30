package questions

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/weitingzhao/bifrost-platform/api/internal/threadinbox"
)

func (s *Service) SetThreadInbox(send threadinbox.Sender) {
	s.mu.Lock()
	s.threadInbox = send
	s.mu.Unlock()
}

// RetryThreadInbox runs even when no question is due to expire. AnswerAsRule
// and Answer share the same business records, so both are reconstructed here.
func (s *Service) RetryThreadInbox(ctx context.Context) {
	s.mu.Lock()
	send := s.threadInbox
	s.mu.Unlock()
	if send == nil {
		return
	}
	if err := s.retryThreadInbox(ctx, send); err != nil {
		slog.Warn("question thread inbox: will retry", "error", err)
	}
}

func (s *Service) retryThreadInbox(ctx context.Context, send threadinbox.Sender) error {
	return s.inboxScan.Run(ctx, func(ctx context.Context) ([]threadinbox.Conclusion, error) {
		raw, err := s.store.readBytesContext(ctx)
		if err != nil {
			return nil, err
		}
		doc, err := parse(raw)
		if err != nil {
			return nil, err
		}
		var items []threadinbox.Conclusion
		for _, q := range doc.Questions {
			if q.Status != StatusAnswered || q.Answer == nil {
				continue
			}
			to := threadinbox.Recipient(q.RequesterThread)
			if to == "" {
				continue
			}
			items = append(items, threadinbox.Conclusion{Key: "question:" + q.ID + ":answered", To: to, Body: questionConclusion(q)})
		}
		return items, nil
	}, send)
}

func questionConclusion(q Question) string {
	option := q.Answer.Option
	for _, o := range q.Options {
		if o.Key == option {
			option += " — " + o.Label
			break
		}
	}
	if option == "" {
		option = "Free-text answer (see Console)"
	}
	return fmt.Sprintf("Question Q%d (%s)\nConclusion: answered\nOption: %s\nAnswered by: %s\nChannel: %s\nAn answer is not an approval.\n%s",
		q.Number, q.ID, threadinbox.Text(option, 220), threadinbox.Text(q.Answer.By, 100),
		threadinbox.Text(q.Answer.Channel, 40), consoleURL(q.ID))
}
