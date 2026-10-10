package agentthreads

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

const (
	// FlushInterval is how often buffered events reach the statefile. Every
	// hook of every session posts here, so writing each one would be a
	// ConfigMap update per tool call.
	FlushInterval = 10 * time.Second
	maxPending    = 2000
)

type stamped struct {
	beat Beat
	at   time.Time
}

// Recorder buffers reported events in the API pod and folds them into the
// store every FlushInterval.
type Recorder struct {
	store   *Store
	cfg     Config
	now     func() time.Time
	mu      sync.Mutex
	pending []stamped
}

func NewRecorder(store *Store, cfg Config) *Recorder {
	return &Recorder{store: store, cfg: cfg, now: time.Now}
}

// Record stamps b with the server clock and buffers it. b must be valid.
func (r *Recorder) Record(b Beat) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.pending) >= maxPending {
		r.pending = r.pending[1:]
	}
	r.pending = append(r.pending, stamped{beat: b, at: r.now().UTC()})
}

// Flush writes the buffered events. On failure they stay buffered.
func (r *Recorder) Flush() error {
	r.mu.Lock()
	batch := r.pending
	r.pending = nil
	r.mu.Unlock()
	if len(batch) == 0 {
		return nil
	}
	cutoff := r.now().Add(-r.cfg.Retain)
	err := r.store.Update(func(st *State) error {
		for _, e := range batch {
			st.Apply(e.beat, e.at)
		}
		st.Prune(cutoff)
		return nil
	})
	if err != nil {
		r.mu.Lock()
		r.pending = append(batch, r.pending...)
		if over := len(r.pending) - maxPending; over > 0 {
			r.pending = r.pending[over:]
		}
		r.mu.Unlock()
	}
	return err
}

// Start flushes every interval until ctx ends.
func (r *Recorder) Start(ctx context.Context, interval time.Duration) {
	go func() {
		defer safego.Recover("agentthreads.flush")
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := r.Flush(); err != nil {
					slog.Warn("agent_threads_flush", "err", err)
				}
			}
		}
	}()
}

// State is the stored state with the buffered events folded in.
func (r *Recorder) State() (State, error) {
	st, err := r.store.Load()
	if err != nil {
		return State{}, err
	}
	r.mu.Lock()
	batch := append([]stamped(nil), r.pending...)
	r.mu.Unlock()
	for _, e := range batch {
		st.Apply(e.beat, e.at)
	}
	return st, nil
}
