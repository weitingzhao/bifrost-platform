package questions

import (
	"context"
	"github.com/weitingzhao/bifrost-platform/api/internal/threadinbox"
	"sync"
)

// Test-only shape from 725/729/733, used to retain the capacity counterexamples.
type legacyInboxEntry struct {
	Record string `json:"record"`
	Order  uint64 `json:"order,omitempty"`
	To     string `json:"to,omitempty"`
	Body   string `json:"body,omitempty"`
	Done   bool   `json:"done,omitempty"`
}
type legacyInboxDocument struct {
	document
	Inbox map[string]legacyInboxEntry `json:"thread_inbox,omitempty"`
}

// Models the sender transaction for offline loop tests. Real PG tests verify
// the receipt/message atomicity, uniqueness, restarts and acknowledgements.
func dedupeInboxSender(send threadinbox.Sender) threadinbox.Sender {
	var mu sync.Mutex
	receipts := map[string]bool{}
	return func(ctx context.Context, key, to, body string) error {
		mu.Lock()
		defer mu.Unlock()
		if receipts[key] {
			return nil
		}
		if err := send(ctx, key, to, body); err != nil {
			return err
		}
		receipts[key] = true
		return nil
	}
}
