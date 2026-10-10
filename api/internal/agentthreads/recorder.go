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

type hostStamped struct {
	beat HostBeat
	at   time.Time
}

// Decision is what Admit tells the handler to return.
type Decision struct {
	// Key is set only when this call started the thread.
	Key string
	// Ignored is an event whose sequence is not newer than the one stored.
	Ignored bool
}

// Recorder buffers reported events in the API pod and folds them into the
// store every FlushInterval.
type Recorder struct {
	store   *Store
	cfg     Config
	now     func() time.Time
	mu      sync.Mutex
	pending []stamped
	hosts   []hostStamped
}

func NewRecorder(store *Store, cfg Config) *Recorder {
	return &Recorder{store: store, cfg: cfg, now: time.Now}
}

// Record stamps b with the server clock and buffers it. b must be valid.
// Record does not check the thread key; Admit does. Tests that build a thread
// directly use Record.
func (r *Recorder) Record(b Beat) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enqueue(b)
}

// Admit buffers b when the caller holds the thread key, or issues a key when
// the thread is new and the caller presented none. The first event is written
// before the key is returned, so a second process cannot start the same thread.
// A wrong or missing key on an existing thread, including one that has no hash
// yet, is ErrKeyRejected. An older sequence is ignored.
func (r *Recorder) Admit(b Beat) (Decision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, err := r.snapshotLocked()
	if err != nil {
		return Decision{}, err
	}
	k := Key(b.Vendor, b.Thread)
	t, seen := st.Threads[k]
	if !seen {
		if b.ThreadKey != "" {
			return Decision{}, ErrKeyRejected
		}
		plain, hash, err := NewThreadKey()
		if err != nil {
			return Decision{}, err
		}
		b.ThreadKey = ""
		b.keyHash = hash
		now := r.now().UTC()
		if err := r.store.Update(func(st *State) error {
			if _, ok := st.Threads[k]; ok {
				return ErrKeyRejected
			}
			st.Apply(b, now)
			return nil
		}); err != nil {
			return Decision{}, err
		}
		return Decision{Key: plain}, nil
	}
	if t.KeyHash == "" || !KeyMatches(t.KeyHash, b.ThreadKey) {
		return Decision{}, ErrKeyRejected
	}
	if b.Seq <= t.Seq {
		return Decision{Ignored: true}, nil
	}
	b.keyHash = t.KeyHash
	b.ThreadKey = ""
	r.enqueue(b)
	return Decision{}, nil
}

// RecordHost buffers one host heartbeat. b must be valid.
func (r *Recorder) RecordHost(b HostBeat) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.hosts) >= maxPending {
		r.hosts = r.hosts[1:]
	}
	r.hosts = append(r.hosts, hostStamped{beat: b, at: r.now().UTC()})
}

func (r *Recorder) enqueue(b Beat) {
	if len(r.pending) >= maxPending {
		r.pending = r.pending[1:]
	}
	r.pending = append(r.pending, stamped{beat: b, at: r.now().UTC()})
}

// Flush writes the buffered events. On failure they stay buffered.
func (r *Recorder) Flush() error {
	r.mu.Lock()
	batch := r.pending
	hosts := r.hosts
	r.pending = nil
	r.hosts = nil
	r.mu.Unlock()
	if len(batch) == 0 && len(hosts) == 0 {
		return nil
	}
	cutoff := r.now().Add(-r.cfg.Retain)
	err := r.store.Update(func(st *State) error {
		for _, e := range batch {
			st.Apply(e.beat, e.at)
		}
		for _, e := range hosts {
			st.ApplyHost(e.beat, e.at)
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
		r.hosts = append(hosts, r.hosts...)
		if over := len(r.hosts) - maxPending; over > 0 {
			r.hosts = r.hosts[over:]
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
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.snapshotLocked()
}

func (r *Recorder) snapshotLocked() (State, error) {
	st, err := r.store.Load()
	if err != nil {
		return State{}, err
	}
	for _, e := range r.pending {
		st.Apply(e.beat, e.at)
	}
	for _, e := range r.hosts {
		st.ApplyHost(e.beat, e.at)
	}
	return st, nil
}
