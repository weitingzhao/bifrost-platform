package approvals

import (
	"context"
	"encoding/json"
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
	file
	Inbox map[string]legacyInboxEntry `json:"thread_inbox,omitempty"`
}

func (doc *legacyInboxDocument) marshalOnce() ([]byte, error) {
	raw, err := doc.file.marshalOnce()
	if err != nil {
		return nil, err
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, err
	}
	if len(doc.Inbox) > 0 {
		top["thread_inbox"], err = json.Marshal(doc.Inbox)
	}
	if err != nil {
		return nil, err
	}
	return writeRawObject(top)
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
