package agentthreads

import (
	"context"
	"errors"
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
// the thread is new. The first event is written before the key is returned.
// A lost first response retries with the same registration nonce and receives
// the same key for registerWindow. A thread with no key yet is migrated by
// issuing one. A pruned thread presented with a stale key is re-registered
// under a new key; the stale key is retired. A live thread whose key the
// caller does not hold is ErrUnknownKey and is not changed. An older turn is
// ErrOlderTurn even when its sequence is newer. An older sequence is ignored.
// A new thread that does not fit among the live ones is ErrThreadFull.
func (r *Recorder) Admit(b Beat) (Decision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now().UTC()
	st, err := r.snapshotLocked()
	if err != nil {
		return Decision{}, err
	}
	if expiredRegisterKey(st, now) {
		if err := r.store.Update(func(st *State) error {
			wipeKeys(st, now)
			return nil
		}); err != nil {
			return Decision{}, err
		}
		st, err = r.snapshotLocked()
		if err != nil {
			return Decision{}, err
		}
	}
	k := Key(b.Vendor, b.Thread)
	t, seen := st.Threads[k]
	// A retry of a lost first response has the nonce and no key. A caller that
	// already holds the key takes the buffered path, including during the window.
	if !seen || t.KeyHash == "" || (b.ThreadKey == "" && nonceLive(t, b.RegisterNonce, now)) {
		return r.admitSync(b, now)
	}
	if !KeyMatches(t.KeyHash, b.ThreadKey) || supersededKey(t, b.ThreadKey) {
		return Decision{}, ErrUnknownKey
	}
	if olderTurn(t, b) {
		return Decision{}, ErrOlderTurn
	}
	if b.Seq <= t.Seq {
		return Decision{Ignored: true}, nil
	}
	b.keyHash = t.KeyHash
	b.ThreadKey = ""
	r.enqueue(b)
	return Decision{}, nil
}

// admitSync writes a registration, a migration, or a nonce replay. The
// plaintext key is returned only to this caller.
func (r *Recorder) admitSync(b Beat, now time.Time) (Decision, error) {
	var decision Decision
	var refused bool
	err := r.store.Update(func(st *State) error {
		decision = Decision{}
		refused = false
		wipeKeys(st, now)
		k := Key(b.Vendor, b.Thread)
		t, seen := st.Threads[k]
		if seen && nonceLive(t, b.RegisterNonce, now) {
			decision.Key = t.RegisterKey
			if b.Seq > t.Seq && !olderTurn(t, b) {
				b.keyHash = t.KeyHash
				b.ThreadKey = ""
				st.Apply(b, now)
			} else {
				decision.Ignored = b.Seq <= t.Seq
			}
			return nil
		}
		if seen && t.KeyHash != "" {
			return ErrUnknownKey
		}
		if seen && t.KeyHash == "" {
			return r.migrate(st, b, now, &decision)
		}
		return r.register(st, b, now, &decision, &refused)
	})
	if refused {
		noteThreadRefused()
		// The registration update rolled back, so the counter is a separate write.
		// A failure here still leaves the caller with ErrThreadFull.
		_ = r.store.Update(func(st *State) error {
			st.ThreadsRefused++
			st.LastThreadRefusal = now
			return nil
		})
	}
	if err != nil {
		return Decision{}, err
	}
	return decision, nil
}

func (r *Recorder) register(st *State, b Beat, now time.Time, d *Decision, refused *bool) error {
	k := Key(b.Vendor, b.Thread)
	if cur, ok := st.Threads[k]; ok {
		if nonceLive(cur, b.RegisterNonce, now) {
			d.Key = cur.RegisterKey
			return nil
		}
		return ErrUnknownKey
	}
	plain, hash, err := NewThreadKey()
	if err != nil {
		return err
	}
	stale := b.ThreadKey
	b.ThreadKey = ""
	b.keyHash = hash
	if !st.Apply(b, now) {
		return errors.New("thread was not stored")
	}
	cur := st.Threads[k]
	cur.RegisterNonce = b.RegisterNonce
	cur.RegisterKey = plain
	cur.RegisteredAt = now
	if stale != "" {
		sum := HashKey(stale)
		if sum != hash {
			cur.SupersededKeys = append(cur.SupersededKeys, sum)
		}
	}
	st.Threads[k] = cur
	if err := st.fitNewThread(k); err != nil {
		*refused = true
		return err
	}
	d.Key = plain
	return nil
}

func (r *Recorder) migrate(st *State, b Beat, now time.Time, d *Decision) error {
	k := Key(b.Vendor, b.Thread)
	cur, ok := st.Threads[k]
	if !ok || cur.KeyHash != "" {
		return ErrUnknownKey
	}
	plain, hash, err := NewThreadKey()
	if err != nil {
		return err
	}
	// A client-supplied key is not adopted. The server issues the key.
	b.ThreadKey = ""
	b.keyHash = hash
	if cur.Seq > 0 && (olderTurn(cur, b) || b.Seq <= cur.Seq) {
		cur.KeyHash = hash
		cur.RegisterNonce = b.RegisterNonce
		cur.RegisterKey = plain
		cur.RegisteredAt = now
		st.Threads[k] = cur
		d.Key = plain
		d.Ignored = true
		return nil
	}
	if !st.Apply(b, now) {
		return errors.New("thread was not stored")
	}
	cur = st.Threads[k]
	cur.RegisterNonce = b.RegisterNonce
	cur.RegisterKey = plain
	cur.RegisteredAt = now
	st.Threads[k] = cur
	d.Key = plain
	return nil
}

func nonceLive(t Thread, nonce string, now time.Time) bool {
	if nonce == "" || nonce != t.RegisterNonce || t.RegisterKey == "" {
		return false
	}
	if t.RegisteredAt.IsZero() || now.Sub(t.RegisteredAt) > registerWindow {
		return false
	}
	return true
}

func supersededKey(t Thread, presented string) bool {
	if presented == "" {
		return false
	}
	for _, h := range t.SupersededKeys {
		if KeyMatches(h, presented) {
			return true
		}
	}
	return false
}

func expiredRegisterKey(st State, now time.Time) bool {
	for _, t := range st.Threads {
		if t.RegisterKey == "" {
			continue
		}
		if t.RegisteredAt.IsZero() || now.Sub(t.RegisteredAt) > registerWindow {
			return true
		}
	}
	return false
}

func wipeKeys(st *State, now time.Time) {
	for k, t := range st.Threads {
		if t.RegisterKey == "" {
			continue
		}
		if t.RegisteredAt.IsZero() || now.Sub(t.RegisteredAt) > registerWindow {
			t.RegisterKey = ""
			st.Threads[k] = t
		}
	}
}

// RecordHost buffers one host heartbeat when it fits. A new host is refused
// with ErrHostFull when every slot is already taken. b must be valid.
func (r *Recorder) RecordHost(b HostBeat) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, err := r.snapshotLocked()
	if err != nil {
		return err
	}
	known := map[string]struct{}{}
	for name := range st.Hosts {
		known[name] = struct{}{}
	}
	for _, h := range r.hosts {
		known[h.beat.Host] = struct{}{}
	}
	if _, ok := known[b.Host]; !ok && len(known) >= maxHosts {
		noteHostRefused()
		return ErrHostFull
	}
	if len(r.hosts) >= maxPending {
		r.hosts = r.hosts[1:]
	}
	r.hosts = append(r.hosts, hostStamped{beat: b, at: r.now().UTC()})
	return nil
}

func (r *Recorder) enqueue(b Beat) {
	if len(r.pending) >= maxPending {
		r.pending = r.pending[1:]
	}
	r.pending = append(r.pending, stamped{beat: b, at: r.now().UTC()})
}

// Flush writes the buffered events. On failure they stay buffered.
// An empty buffer still drops registration plaintext whose window has
// passed, and that path writes only when a key is actually expired.
func (r *Recorder) Flush() error {
	r.mu.Lock()
	batch := r.pending
	hosts := r.hosts
	r.pending = nil
	r.hosts = nil
	now := r.now().UTC()
	r.mu.Unlock()
	if len(batch) == 0 && len(hosts) == 0 {
		return r.wipeExpiredRegisterKeys(now)
	}
	cutoff := now.Add(-r.cfg.Retain)
	var refusedThreads, refusedHosts int
	err := r.store.Update(func(st *State) error {
		refusedThreads, refusedHosts = 0, 0
		wipeKeys(st, now)
		for _, e := range batch {
			k := Key(e.beat.Vendor, e.beat.Thread)
			_, seen := st.Threads[k]
			if !st.Apply(e.beat, e.at) {
				continue
			}
			if !seen {
				if ferr := st.fitNewThread(k); ferr != nil {
					delete(st.Threads, k)
					st.ThreadsRefused++
					st.LastThreadRefusal = now
					refusedThreads++
				}
			}
		}
		for _, e := range hosts {
			if ferr := st.fitNewHost(e.beat.Host); ferr != nil {
				refusedHosts++
				continue
			}
			st.ApplyHost(e.beat, e.at)
		}
		st.Prune(cutoff, r.cfg, now)
		return nil
	})
	if err == nil {
		for i := 0; i < refusedThreads; i++ {
			noteThreadRefused()
		}
		for i := 0; i < refusedHosts; i++ {
			noteHostRefused()
		}
	}
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

// errNothingExpired tells Update not to write when a concurrent pass already
// cleared the plaintext keys.
var errNothingExpired = errors.New("nothing expired")

// wipeExpiredRegisterKeys removes registration plaintext after the window.
// It does not write when every key is still inside the window.
func (r *Recorder) wipeExpiredRegisterKeys(now time.Time) error {
	st, err := r.store.Load()
	if err != nil {
		return err
	}
	if !expiredRegisterKey(st, now) {
		return nil
	}
	err = r.store.Update(func(st *State) error {
		if !expiredRegisterKey(*st, now) {
			return errNothingExpired
		}
		wipeKeys(st, now)
		return nil
	})
	if errors.Is(err, errNothingExpired) {
		return nil
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
