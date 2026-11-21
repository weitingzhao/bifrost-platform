package questions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
)

// The thread inbox (telling a thread how its question was answered) is the
// coordination domain's (package threadinbox). questions declares the port and
// the server binds it (server/thread_inbox.go), so this package does not import
// another domain (the foundation line's wall debt, 2026-11-21). The three
// types are aliases of plain types, so the binding can be written without
// naming this package (the server may not; deps_test.go).

// InboxSender commits a message and its receipt together (threadinbox.Sender).
type InboxSender = func(ctx context.Context, key, to, body string) error

// InboxItem is one conclusion for one thread (threadinbox.Conclusion).
type InboxItem = struct{ Key, To, Body string }

// InboxRunner runs one delivery pass over what load returns (threadinbox.Scan.Run).
type InboxRunner = func(ctx context.Context, load func(context.Context) ([]InboxItem, error), send InboxSender) error

// InboxPorts is the thread inbox as questions uses it; NewRunner makes one
// runner per Service (a runner keeps its own scan cursor).
type InboxPorts struct {
	Recipient func(label string) string
	Text      func(s string, limit int) string
	NewRunner func() InboxRunner
}

var (
	inboxPortsMu sync.RWMutex
	inboxPorts   InboxPorts
)

// SetInboxPorts binds the thread inbox. Unbound, no conclusion is delivered.
func SetInboxPorts(p InboxPorts) {
	inboxPortsMu.Lock()
	inboxPorts = p
	inboxPortsMu.Unlock()
}

func boundInboxPorts() (InboxPorts, bool) {
	inboxPortsMu.RLock()
	defer inboxPortsMu.RUnlock()
	p := inboxPorts
	return p, p.Recipient != nil && p.Text != nil && p.NewRunner != nil
}

func inboxText(s string, limit int) string {
	if p, ok := boundInboxPorts(); ok {
		return p.Text(s, limit)
	}
	return ""
}

func (s *Service) inboxRunner(p InboxPorts) InboxRunner {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inboxRun == nil {
		s.inboxRun = p.NewRunner()
	}
	return s.inboxRun
}

func (s *Service) SetThreadInbox(send InboxSender) {
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

func (s *Service) retryThreadInbox(ctx context.Context, send InboxSender) error {
	p, ok := boundInboxPorts()
	if !ok {
		return errors.New("the thread inbox is not bound")
	}
	return s.inboxRunner(p)(ctx, func(ctx context.Context) ([]InboxItem, error) {
		raw, err := s.store.readBytesContext(ctx)
		if err != nil {
			return nil, err
		}
		doc, err := parse(raw)
		if err != nil {
			return nil, err
		}
		var items []InboxItem
		for _, q := range doc.Questions {
			if q.Status != StatusAnswered || q.Answer == nil {
				continue
			}
			to := p.Recipient(q.RequesterThread)
			if to == "" {
				continue
			}
			items = append(items, InboxItem{Key: "question:" + q.ID + ":answered", To: to, Body: questionConclusion(q)})
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
		q.Number, q.ID, inboxText(option, 220), inboxText(q.Answer.By, 100),
		inboxText(q.Answer.Channel, 40), consoleURL(q.ID))
}
