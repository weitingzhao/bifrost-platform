package approvals

import (
	"context"
	"time"
)

// RunHeldFinishes retries only results computed by this process. API roles
// use it instead of RunRetries: it never claims actions or sends reminders.
// Workers already flush in retryDue and must not start this loop as well.
func (s *Service) RunHeldFinishes(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if ctx.Err() != nil {
				return
			}
			s.flushHeld()
			s.RetryThreadInbox(ctx)
		}
	}
}

// FlushHeldFinishes writes, once, any result this process computed but could
// not store. The server calls it on shutdown after the loop has stopped.
func (s *Service) FlushHeldFinishes() {
	s.flushHeld()
}
