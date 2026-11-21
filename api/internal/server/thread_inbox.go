package server

import (
	"context"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	ownerassembly "github.com/weitingzhao/bifrost-platform/api/internal/owner/assembly"
	"github.com/weitingzhao/bifrost-platform/api/internal/threadinbox"
)

// The thread inbox is the coordination domain's (threadinbox); approvals and
// questions declare a port each and are bound here, so neither imports another
// domain (the foundation line's wall debt, 2026-11-21).
func init() {
	approvals.SetInboxPorts(approvals.InboxPorts{
		Recipient: threadinbox.Recipient,
		Text:      threadinbox.Text,
		NewRunner: func() approvals.InboxRunner {
			var scan threadinbox.Scan
			return func(ctx context.Context, load func(context.Context) ([]approvals.InboxItem, error), send approvals.InboxSender) error {
				return scan.Run(ctx, func(ctx context.Context) ([]threadinbox.Conclusion, error) {
					items, err := load(ctx)
					out := make([]threadinbox.Conclusion, len(items))
					for i, it := range items {
						out[i] = threadinbox.Conclusion{Key: it.Key, To: it.To, Body: it.Body}
					}
					return out, err
				}, threadinbox.Sender(send))
			}
		},
	})
	ownerassembly.BindQuestionInbox(threadinbox.Recipient, threadinbox.Text,
		func() func(context.Context, func(context.Context) ([]inboxItem, error), func(context.Context, string, string, string) error) error {
			var scan threadinbox.Scan
			return func(ctx context.Context, load func(context.Context) ([]inboxItem, error), send func(context.Context, string, string, string) error) error {
				return scan.Run(ctx, func(ctx context.Context) ([]threadinbox.Conclusion, error) {
					items, err := load(ctx)
					out := make([]threadinbox.Conclusion, len(items))
					for i, it := range items {
						out[i] = threadinbox.Conclusion{Key: it.Key, To: it.To, Body: it.Body}
					}
					return out, err
				}, threadinbox.Sender(send))
			}
		})
}

// inboxItem is the questions port's item, written as its plain type.
type inboxItem = struct{ Key, To, Body string }

// The late DB keeps an unavailable runtime retryable. Both processes can
// deliver; PostgreSQL, not a process-local flag, owns deduplication.
func (s *Server) wireThreadInbox(db eventlog.DB) {
	send := func(ctx context.Context, key, to, body string) error {
		return coord.PostConclusion(ctx, db, key, to, body)
	}
	s.approvals.SetThreadInbox(send)
	s.questionRuntime.Svc.SetThreadInbox(send)
}
