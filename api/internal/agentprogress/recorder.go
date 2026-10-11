package agentprogress

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// FlushInterval is how often a buffered document reaches the statefile. The
// publisher posts on its own cadence; writing each one would be a ConfigMap
// update per publish. Only the latest document is kept.
const FlushInterval = 10 * time.Second

// Recorder buffers the latest document and folds it into the store on Flush.
type Recorder struct {
	store *Store
	now   func() time.Time
	mu    sync.Mutex
	// pending is the newest document not yet confirmed in the store.
	pending *Snapshot
}

func NewRecorder(store *Store) *Recorder {
	return &Recorder{store: store, now: time.Now}
}

// Publish keeps doc as the latest document. A newer call replaces an older
// one that has not been flushed yet. A snapshot over MaxDocumentBytes is
// refused and the previous document stays.
func (r *Recorder) Publish(doc json.RawMessage, host string) error {
	snap := Snapshot{
		Document:   append(json.RawMessage(nil), doc...),
		ReceivedAt: r.now().UTC(),
		Host:       host,
	}
	raw, err := marshalJSON(snap)
	if err != nil {
		return err
	}
	if int64(len(raw)) > MaxDocumentBytes {
		return errTooLarge
	}
	r.mu.Lock()
	r.pending = &snap
	r.mu.Unlock()
	return nil
}

// Flush writes the buffered document. A newer Publish during the write stays
// buffered. On failure the document stays buffered.
func (r *Recorder) Flush() error {
	r.mu.Lock()
	p := r.pending
	r.mu.Unlock()
	if p == nil {
		return nil
	}
	if err := r.store.Save(*p); err != nil {
		return err
	}
	r.mu.Lock()
	if r.pending == p {
		r.pending = nil
	}
	r.mu.Unlock()
	return nil
}

// Current is the buffered document, or the stored one when nothing is buffered.
func (r *Recorder) Current() (Snapshot, bool, error) {
	r.mu.Lock()
	p := r.pending
	r.mu.Unlock()
	if p != nil {
		out := *p
		out.Document = append(json.RawMessage(nil), p.Document...)
		return out, true, nil
	}
	return r.store.Load()
}

// Start flushes every interval until ctx ends.
func (r *Recorder) Start(ctx context.Context, interval time.Duration) {
	go func() {
		defer safego.Recover("agentprogress.flush")
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := r.Flush(); err != nil {
					slog.Warn("agent_progress_flush", "err", err)
				}
			}
		}
	}()
}
