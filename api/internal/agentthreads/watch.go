package agentthreads

import (
	"context"
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
		if claimed, err := w.claim(k, st.Threads[k].At, now); err != nil {
			slog.Warn("agent_threads_watch", "mark", k, "err", err)
			ok = false
		} else if claimed {
			countPush(PushStale)
		}
	}
	if len(push) > 0 {
		titles := w.lineageTitles(ctx)
		ok = w.pushThreads(ctx, st, push, titles, now) && ok
	}
	hPush, hMark := w.cfg.HostsDue(st, now)
	for _, name := range hMark {
		if claimed, err := w.claimHost(name, st.Hosts[name].At, now); err != nil {
			slog.Warn("agent_threads_watch", "host_mark", name, "err", err)
			ok = false
		} else if claimed {
			countHostPush(PushStale)
		}
	}
	for _, name := range hPush {
		h := st.Hosts[name]
		claimed, err := w.claimHost(name, h.At, now)
		if err != nil {
			slog.Warn("agent_threads_watch", "host_claim", name, "err", err)
			ok = false
			continue
		}
		if !claimed {
			continue
		}
		subject, body := w.cfg.HostMessage(h, w.cfg.MidTurn(st, name), now)
		slog.Info("agent_host_lost", "host", name, "quiet", now.Sub(h.At).String())
		if w.notify == nil {
			continue
		}
		if err := w.notify(ctx, subject, body); err != nil {
			slog.Warn("agent_threads_watch", "host_push", name, "err", err)
			countHostPush(PushFailed)
			if uerr := w.unclaimHost(name, h.At); uerr != nil {
				slog.Warn("agent_threads_watch", "host_unclaim", name, "err", uerr)
			}
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
		claimed, err := w.claim(k, t.At, now)
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
		slog.Info("agent_thread_silent", "thread", k, "host", t.Host, "last_event", t.Event, "quiet", now.Sub(t.At).String())
		if w.notify == nil {
			continue
		}
		if err := w.notify(ctx, subject, body); err != nil {
			slog.Warn("agent_threads_watch", "push", k, "err", err)
			countPush(PushFailed)
			if uerr := w.unclaim(k, t.At); uerr != nil {
				slog.Warn("agent_threads_watch", "unclaim", k, "err", uerr)
			}
			ok = false
			continue
		}
		countPush(PushSent)
	}
	return ok
}

// claim marks the silence that started with the event at `at` as pushed,
// unless a newer event arrived or someone already marked it. Marking before
// pushing keeps two overlapping workers pods (a rollout) from both pushing.
func (w *Watcher) claim(key string, at, now time.Time) (bool, error) {
	claimed := false
	err := w.store.Update(func(st *State) error {
		claimed = false
		t, found := st.Threads[key]
		if !found || !t.At.Equal(at) || t.Pushed() {
			return nil
		}
		t.NotifiedFor, t.NotifiedAt = at, now
		st.Threads[key] = t
		claimed = true
		return nil
	})
	return claimed, err
}

// unclaim undoes claim after a failed push, so the next pass retries.
func (w *Watcher) unclaim(key string, at time.Time) error {
	return w.store.Update(func(st *State) error {
		t, found := st.Threads[key]
		if !found || !t.NotifiedFor.Equal(at) {
			return nil
		}
		t.NotifiedFor, t.NotifiedAt = time.Time{}, time.Time{}
		st.Threads[key] = t
		return nil
	})
}

func (w *Watcher) claimHost(name string, at, now time.Time) (bool, error) {
	claimed := false
	err := w.store.Update(func(st *State) error {
		claimed = false
		h, found := st.Hosts[name]
		if !found || !h.At.Equal(at) || h.LostNotifiedFor.Equal(at) {
			return nil
		}
		h.LostNotifiedFor, h.LostNotifiedAt = at, now
		st.Hosts[name] = h
		claimed = true
		return nil
	})
	return claimed, err
}

func (w *Watcher) unclaimHost(name string, at time.Time) error {
	return w.store.Update(func(st *State) error {
		h, found := st.Hosts[name]
		if !found || !h.LostNotifiedFor.Equal(at) {
			return nil
		}
		h.LostNotifiedFor, h.LostNotifiedAt = time.Time{}, time.Time{}
		st.Hosts[name] = h
		return nil
	})
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
	metricsMu   sync.Mutex
	byStatus    = map[Status]int{InTurn: 0, Silent: 0, StatusWaiting: 0, HostLost: 0}
	pushes      = map[string]int64{PushSent: 0, PushFailed: 0, PushStale: 0}
	hostPushes  = map[string]int64{PushSent: 0, PushFailed: 0, PushStale: 0}
	gaugeStatus = []Status{InTurn, StatusWaiting, Silent, HostLost}
)

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
