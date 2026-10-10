package agentthreads

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/maintainer"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// WatchInterval is how often the workers loop judges.
const WatchInterval = 30 * time.Second

// claimTTL is how long a send stays claimed before a dead worker's claim can
// be taken. Two minutes is long enough to finish a push and short enough that
// a crash is retried on a later pass. The stricter choice is this short lease
// rather than a claim that never expires.
const claimTTL = 2 * time.Minute

// Push results, the label values of the pushes counter.
const (
	PushSent   = "sent"
	PushFailed = "failed"
	PushStale  = "stale"
)

// Notify pushes one message to the Owner's phone.
type Notify func(ctx context.Context, title, body string) error

// TitlesFunc returns thread id -> title from the lineage titles (Claude
// sessions report theirs there, TD-197). Nil or an error leaves reported titles.
type TitlesFunc func(ctx context.Context) (map[string]string, error)

// WatchWanted says whether this process pushes. The phone is paged from PROD
// only: by default only OPS_VIEWER_ENV=prod; PLATFORM_AGENT_THREAD_WATCH=on|off overrides.
func WatchWanted() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PLATFORM_AGENT_THREAD_WATCH"))) {
	case "on", "true", "1":
		return true
	case "off", "false", "0":
		return false
	}
	return strings.EqualFold(strings.TrimSpace(os.Getenv("OPS_VIEWER_ENV")), "prod")
}

// Watcher is the workers loop: judge, push once per silence.
type Watcher struct {
	store  *Store
	cfg    Config
	notify Notify
	titles TitlesFunc
	now    func() time.Time
}

func NewWatcher(store *Store, cfg Config, notify Notify, titles TitlesFunc) *Watcher {
	return &Watcher{store: store, cfg: cfg, notify: notify, titles: titles, now: time.Now}
}

// Start runs Tick now and then every interval until ctx ends.
func (w *Watcher) Start(ctx context.Context, interval time.Duration) {
	go func() {
		defer safego.Recover("agentthreads.watch")
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			w.Tick(ctx)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

// Tick judges once. A pass that read the state and pushed what was due is a
// maintainer success.
func (w *Watcher) Tick(ctx context.Context) {
	id := maintainer.PlatformID(maintainer.LoopAgentThreads)
	if w.tick(ctx) {
		maintainer.Success(id)
	} else {
		maintainer.Failure(id)
	}
}

func (w *Watcher) tick(ctx context.Context) bool {
	now := w.now().UTC()
	st, err := w.store.Load()
	if err != nil {
		slog.Warn("agent_threads_watch", "err", err)
		return false
	}
	setGauges(w.cfg, st, now)
	push, mark := w.cfg.Due(st, now)
	ok := true
	for _, k := range mark {
		if marked, err := w.mark(k, st.Threads[k].At, now); err != nil {
			slog.Warn("agent_threads_watch", "mark", k, "err", err)
			ok = false
		} else if marked {
			countPush(PushStale)
		}
	}
	if len(push) > 0 {
		titles := w.lineageTitles(ctx)
		ok = w.pushThreads(ctx, st, push, titles, now) && ok
	}
	hPush, hMark := w.cfg.HostsDue(st, now)
	for _, name := range hMark {
		if marked, err := w.markHost(name, st.Hosts[name].At, now); err != nil {
			slog.Warn("agent_threads_watch", "host_mark", name, "err", err)
			ok = false
		} else if marked {
			countHostPush(PushStale)
		}
	}
	for _, name := range hPush {
		h := st.Hosts[name]
		id, claimed, err := w.claimHost(name, h.At, now)
		if err != nil {
			slog.Warn("agent_threads_watch", "host_claim", name, "err", err)
			ok = false
			continue
		}
		if !claimed {
			continue
		}
		subject, body := w.cfg.HostMessage(h, w.cfg.MidTurn(st, name), now)
		slog.Info("agent_host_lost", "host", name, "quiet", now.Sub(h.At).String(), "claim", id)
		// Retryable delivery, not exactly once. The relay has no dedup id:
		// a crash after notify returns and before complete sends this again.
		if w.notify == nil {
			if cerr := w.completeHost(name, id, h.At, now); cerr != nil {
				slog.Warn("agent_threads_watch", "host_complete", name, "err", cerr)
				ok = false
			}
			continue
		}
		if err := w.notify(ctx, subject, body); err != nil {
			slog.Warn("agent_threads_watch", "host_push", name, "err", err)
			countHostPush(PushFailed)
			if uerr := w.releaseHost(name, id); uerr != nil {
				slog.Warn("agent_threads_watch", "host_release", name, "err", uerr)
			}
			ok = false
			continue
		}
		if cerr := w.completeHost(name, id, h.At, now); cerr != nil {
			slog.Warn("agent_threads_watch", "host_complete", name, "err", cerr)
			ok = false
			continue
		}
		countHostPush(PushSent)
	}
	return ok
}

func (w *Watcher) pushThreads(ctx context.Context, st State, push []string, titles map[string]string, now time.Time) bool {
	ok := true
	for _, k := range push {
		t := st.Threads[k]
		id, claimed, err := w.claim(k, t.At, now)
		if err != nil {
			slog.Warn("agent_threads_watch", "claim", k, "err", err)
			ok = false
			continue
		}
		if !claimed {
			continue
		}
		if title, found := titles[t.Thread]; found {
			t.Title = title
		}
		subject, body := w.cfg.Message(t, now)
		slog.Info("agent_thread_silent", "thread", k, "host", t.Host, "last_event", t.Event, "quiet", now.Sub(t.At).String(), "claim", id)
		// Retryable delivery, not exactly once. The relay has no dedup id:
		// a crash after notify returns and before complete sends this again.
		if w.notify == nil {
			if cerr := w.complete(k, id, t.At, now); cerr != nil {
				slog.Warn("agent_threads_watch", "complete", k, "err", cerr)
				ok = false
			}
			continue
		}
		if err := w.notify(ctx, subject, body); err != nil {
			slog.Warn("agent_threads_watch", "push", k, "err", err)
			countPush(PushFailed)
			if uerr := w.release(k, id); uerr != nil {
				slog.Warn("agent_threads_watch", "release", k, "err", uerr)
			}
			ok = false
			continue
		}
		if cerr := w.complete(k, id, t.At, now); cerr != nil {
			slog.Warn("agent_threads_watch", "complete", k, "err", cerr)
			ok = false
			continue
		}
		countPush(PushSent)
	}
	return ok
}

// claim records a send attempt for the silence at `at`. It does not mark the
// silence sent. A live claim blocks a second worker; an expired claim can be
// taken. The returned id must be presented to complete or release.
func (w *Watcher) claim(key string, at, now time.Time) (string, bool, error) {
	id := newClaimID()
	claimed := false
	err := w.store.Update(func(st *State) error {
		claimed = false
		t, found := st.Threads[key]
		if !found || !t.At.Equal(at) || t.Pushed() || t.silenceClaimed(now) {
			return nil
		}
		t.SilenceClaimID = id
		t.SilenceClaimExpires = now.Add(claimTTL)
		st.Threads[key] = t
		claimed = true
		return nil
	})
	return id, claimed, err
}

// complete marks the silence sent. The claim id must still be the one this
// worker holds, and the event time must be unchanged.
func (w *Watcher) complete(key, id string, at, now time.Time) error {
	return w.store.Update(func(st *State) error {
		t, found := st.Threads[key]
		if !found || t.SilenceClaimID != id || !t.At.Equal(at) {
			return nil
		}
		t.NotifiedFor, t.NotifiedAt = at, now
		t.SilenceClaimID = ""
		t.SilenceClaimExpires = time.Time{}
		st.Threads[key] = t
		return nil
	})
}

// release drops a claim after a failed push so the next pass can retry.
// A different claim id is left alone.
func (w *Watcher) release(key, id string) error {
	return w.store.Update(func(st *State) error {
		t, found := st.Threads[key]
		if !found || t.SilenceClaimID != id {
			return nil
		}
		t.SilenceClaimID = ""
		t.SilenceClaimExpires = time.Time{}
		st.Threads[key] = t
		return nil
	})
}

// mark records a silence that is too old to page. There is no send, so this
// is the sent mark in one write. A live claim is left to its holder.
func (w *Watcher) mark(key string, at, now time.Time) (bool, error) {
	marked := false
	err := w.store.Update(func(st *State) error {
		marked = false
		t, found := st.Threads[key]
		if !found || !t.At.Equal(at) || t.Pushed() || t.silenceClaimed(now) {
			return nil
		}
		t.NotifiedFor, t.NotifiedAt = at, now
		t.SilenceClaimID = ""
		t.SilenceClaimExpires = time.Time{}
		st.Threads[key] = t
		marked = true
		return nil
	})
	return marked, err
}

func (w *Watcher) claimHost(name string, at, now time.Time) (string, bool, error) {
	id := newClaimID()
	claimed := false
	err := w.store.Update(func(st *State) error {
		claimed = false
		h, found := st.Hosts[name]
		if !found || !h.At.Equal(at) || h.LostNotifiedFor.Equal(at) || claimPending(h.LossClaimID, h.LossClaimExpires, now) {
			return nil
		}
		h.LossClaimID = id
		h.LossClaimExpires = now.Add(claimTTL)
		st.Hosts[name] = h
		claimed = true
		return nil
	})
	return id, claimed, err
}

func (w *Watcher) completeHost(name, id string, at, now time.Time) error {
	return w.store.Update(func(st *State) error {
		h, found := st.Hosts[name]
		if !found || h.LossClaimID != id || !h.At.Equal(at) {
			return nil
		}
		h.LostNotifiedFor, h.LostNotifiedAt = at, now
		h.LossClaimID = ""
		h.LossClaimExpires = time.Time{}
		st.Hosts[name] = h
		return nil
	})
}

func (w *Watcher) releaseHost(name, id string) error {
	return w.store.Update(func(st *State) error {
		h, found := st.Hosts[name]
		if !found || h.LossClaimID != id {
			return nil
		}
		h.LossClaimID = ""
		h.LossClaimExpires = time.Time{}
		st.Hosts[name] = h
		return nil
	})
}

func (w *Watcher) markHost(name string, at, now time.Time) (bool, error) {
	marked := false
	err := w.store.Update(func(st *State) error {
		marked = false
		h, found := st.Hosts[name]
		if !found || !h.At.Equal(at) || h.LostNotifiedFor.Equal(at) || claimPending(h.LossClaimID, h.LossClaimExpires, now) {
			return nil
		}
		h.LostNotifiedFor, h.LostNotifiedAt = at, now
		h.LossClaimID = ""
		h.LossClaimExpires = time.Time{}
		st.Hosts[name] = h
		marked = true
		return nil
	})
	return marked, err
}

func newClaimID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(buf[:])
}

func (w *Watcher) lineageTitles(ctx context.Context) map[string]string {
	if w.titles == nil {
		return nil
	}
	m, err := w.titles(ctx)
	if err != nil {
		slog.Warn("agent_threads_watch", "titles", err)
		return nil
	}
	return m
}

var (
	metricsMu      sync.Mutex
	byStatus       = map[Status]int{InTurn: 0, Silent: 0, StatusWaiting: 0, HostLost: 0}
	pushes         = map[string]int64{PushSent: 0, PushFailed: 0, PushStale: 0}
	hostPushes     = map[string]int64{PushSent: 0, PushFailed: 0, PushStale: 0}
	refusedThreads int64
	refusedHosts   int64
	gaugeStatus    = []Status{InTurn, StatusWaiting, Silent, HostLost}
)

func noteThreadRefused() {
	metricsMu.Lock()
	refusedThreads++
	metricsMu.Unlock()
}

func noteHostRefused() {
	metricsMu.Lock()
	refusedHosts++
	metricsMu.Unlock()
}

// RegistrationRefusals is how many new threads and hosts were refused because
// the live cap was full. It is process-local, like the push counters.
func RegistrationRefusals() (threads, hosts int64) {
	metricsMu.Lock()
	defer metricsMu.Unlock()
	return refusedThreads, refusedHosts
}

func setGauges(cfg Config, st State, now time.Time) {
	counts := map[Status]int{InTurn: 0, Silent: 0, StatusWaiting: 0, HostLost: 0}
	for _, t := range st.Threads {
		if s := cfg.StatusOf(t, cfg.HostDown(st, t.Host, now), now); s != Idle {
			counts[s]++
		}
	}
	metricsMu.Lock()
	byStatus = counts
	metricsMu.Unlock()
}

func countPush(result string) {
	metricsMu.Lock()
	pushes[result]++
	metricsMu.Unlock()
}

func countHostPush(result string) {
	metricsMu.Lock()
	hostPushes[result]++
	metricsMu.Unlock()
}

// WriteMetrics appends the agent thread series to a /metrics body. Only the
// process that runs the watcher has non-zero values.
func WriteMetrics(b *strings.Builder) {
	metricsMu.Lock()
	defer metricsMu.Unlock()
	b.WriteString("# HELP bifrost_agent_threads Agent threads mid-turn, by status, as of the last watch pass\n")
	b.WriteString("# TYPE bifrost_agent_threads gauge\n")
	for _, s := range gaugeStatus {
		fmt.Fprintf(b, "bifrost_agent_threads{status=%q} %d\n", s, byStatus[s])
	}
	b.WriteString("# HELP bifrost_agent_thread_silence_pushes_total Silent agent threads handled, by result (sent, failed, stale = too old to page)\n")
	b.WriteString("# TYPE bifrost_agent_thread_silence_pushes_total counter\n")
	writePushCounter(b, "bifrost_agent_thread_silence_pushes_total", pushes)
	b.WriteString("# HELP bifrost_agent_host_lost_pushes_total Host losses handled, by result (sent, failed, stale = too old to page)\n")
	b.WriteString("# TYPE bifrost_agent_host_lost_pushes_total counter\n")
	writePushCounter(b, "bifrost_agent_host_lost_pushes_total", hostPushes)
	b.WriteString("# HELP bifrost_agent_thread_registrations_refused_total New thread or host registrations refused because the live cap was full\n")
	b.WriteString("# TYPE bifrost_agent_thread_registrations_refused_total counter\n")
	fmt.Fprintf(b, "bifrost_agent_thread_registrations_refused_total{kind=%q} %d\n", "thread", refusedThreads)
	fmt.Fprintf(b, "bifrost_agent_thread_registrations_refused_total{kind=%q} %d\n", "host", refusedHosts)
}

func writePushCounter(b *strings.Builder, name string, counts map[string]int64) {
	results := make([]string, 0, len(counts))
	for r := range counts {
		results = append(results, r)
	}
	sort.Strings(results)
	for _, r := range results {
		fmt.Fprintf(b, "%s{result=%q} %d\n", name, r, counts[r])
	}
}
