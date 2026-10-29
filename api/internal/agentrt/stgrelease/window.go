package stgrelease

import (
	"context"
	"errors"
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"k8s.io/utils/clock"
)

var (
	ErrWindowDisabled = errors.New("STG window holding is disabled")
	ErrWindowWait     = errors.New("STG window requires waiting or observation")
)

// Windows is the injected, bounded ConfigMap CAS transport. No operation
// retries a write against a newer version. The production implementation is
// delivery.STGReleaseAdapter using the PROD workers' client identity.
type Windows interface {
	ReadSTGWindow(context.Context) (delivery.STGWindow, error)
	TakeSTGWindow(context.Context, delivery.STGWindow, string, int64, bool) (delivery.STGWindow, error)
	RenewSTGWindow(context.Context, delivery.STGWindow) (delivery.STGWindow, error)
	EndSTGWindow(context.Context, delivery.STGWindow) error
}

// WindowPolicy is the current local switch/rule view, not an authorization to
// send. Policy must be concurrency-safe, perform no I/O, and return immutable
// rules freshly selected by its caller. The B5-c final gate still re-reads the
// authoritative rule under its transaction locks.
type WindowPolicy struct {
	Environment string
	Live        bool
	Rule        *rules.Rule
}

func (p WindowPolicy) enabled(at time.Time) bool {
	r := p.Rule
	if p.Environment != "prod" || !p.Live || r == nil || r.Kind != rules.KindLanding || r.Version < 1 || r.Landing == nil || at.Before(r.Effective) || !at.Before(r.Expires) {
		return false
	}
	repo, ok := r.Landing.Repo("bifrost-platform")
	return ok && repo.STGRelease == delivery.STGReleasePipeline
}

type WindowConfig struct {
	Windows Windows
	Policy  func() WindowPolicy
	Clock   clock.WithTicker
}

// WindowLoss is consumed asynchronously, outside the renewer, with
// store.RecordStgLeaseLost. Loss never means failed/verified/settled: the run
// observer must continue even when live is off or the rule becomes none.
type WindowLoss struct {
	Batch           string
	Try, Generation int64
	Err             error
}

// WindowHolder is one-shot and belongs to one try. After loss or Release it
// cannot reacquire or renew. A new recovery holder must replay the stream.
type WindowHolder struct {
	cfg             WindowConfig
	batch, who      string
	try             int64
	mu              sync.Mutex
	io              sync.Mutex
	held            delivery.STGWindow
	active, lost    bool
	releaseErr      error
	cancel          context.CancelFunc
	renewDone, done chan struct{}
	losses          chan WindowLoss
}

// HoldWindow is explicit preparation for selection/final gate or observation
// of an outstanding started intent. It starts only a lease renewer, never the
// B5-e controller. The caller must Release before CI waiting, blocking,
// backoff, or verification; terminal/quarantined tries cannot acquire here.
func HoldWindow(ctx context.Context, cfg WindowConfig, state store.StgState, batch string, try int64) (*WindowHolder, error) {
	if cfg.Clock == nil {
		cfg.Clock = clock.RealClock{}
	}
	if cfg.Policy == nil || !cfg.Policy().enabled(cfg.Clock.Now()) {
		return nil, ErrWindowDisabled // none does not even read the window
	}
	if cfg.Windows == nil {
		return nil, errors.New("STG window transport unavailable")
	}
	who, floor, recover, err := windowIntent(state, batch, try, cfg.Clock.Now())
	if err != nil {
		return nil, err
	}
	readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	old, err := cfg.Windows.ReadSTGWindow(readCtx)
	cancel()
	if err != nil {
		return nil, err
	}
	// Restart can only adopt the recorded try's own window. A foreign or
	// absent window leaves the existing execution to the read-only observer.
	if recover && (old.Who != who || old.What != "bifrost-platform" || old.Env != "stg" || old.Generation < 1) {
		return nil, ErrWindowWait
	}
	if old.Generation > floor {
		floor = old.Generation
	}
	// JSON event integers must round-trip without losing precision.
	if floor >= 1<<53-1 {
		return nil, errors.New("STG window generation exhausted")
	}
	if !cfg.Policy().enabled(cfg.Clock.Now()) {
		return nil, ErrWindowDisabled
	}
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	held, err := cfg.Windows.TakeSTGWindow(writeCtx, old, who, floor+1, recover)
	cancel()
	if err != nil {
		return nil, err
	}
	renewCtx, stop := context.WithCancel(ctx)
	h := &WindowHolder{cfg: cfg, batch: batch, who: who, try: try, held: held, active: true,
		cancel: stop, renewDone: make(chan struct{}), done: make(chan struct{}), losses: make(chan WindowLoss, 1)}
	// Create the ticker before returning so no first tick can be missed.
	ticker := cfg.Clock.NewTicker(time.Minute)
	safego.Go("stg-release-window-renew", func() { h.renew(renewCtx, ticker) })
	finishCtx := context.WithoutCancel(ctx)
	safego.Go("stg-release-window-finish", func() { h.finish(finishCtx) })
	if !cfg.Policy().enabled(cfg.Clock.Now()) || ctx.Err() != nil {
		_ = h.Release()
		return nil, ErrWindowDisabled
	}
	return h, nil
}

func windowIntent(s store.StgState, batch string, try int64, at time.Time) (who string, floor int64, recover bool, err error) {
	n, e := strconv.ParseInt(strings.TrimPrefix(batch, "stg:bifrost-platform:"), 10, 64)
	b := s.Batches[batch]
	if e != nil || n < 1 || batch != fmt.Sprintf("stg:bifrost-platform:%d", n) || try < 1 || b == nil {
		return "", 0, false, ErrWindowWait
	}
	diagnosticTry := b.Try // OPEN diagnostics use try=0, per the event matrix
	t := b.Tries[try]
	switch {
	case b.Phase == "OPEN" && b.Try == 0 && try == 1:
	case b.Phase == "SNAP" && b.Try == try && t != nil && !at.Before(t.Snapshot.Record.Time("not_before")):
	case b.Try == try && t.Occupied() && !t.IsQuarantined():
		recover = true
		floor = object(t.Permit.Record, "lease").Int("gen")
		if object(t.Started.Record, "lease").Int("gen") > floor {
			floor = object(t.Started.Record, "lease").Int("gen")
		}
	default:
		return "", 0, false, ErrWindowWait
	}
	who = fmt.Sprintf("stgrel-bifrost-platform-%d-%d", n, try)
	if recover && (object(t.Started.Record, "lease").String("who") != who || floor < 1) {
		return "", 0, false, ErrWindowWait
	}
	for _, diag := range b.Diagnostics {
		if diag.Record.Int("try") == diagnosticTry && diag.Record.Int("gen") > floor {
			floor = diag.Record.Int("gen")
		}
	}
	return who, floor, recover, nil
}

func (h *WindowHolder) Losses() <-chan WindowLoss { return h.losses }
func (h *WindowHolder) Done() <-chan struct{}     { return h.done }

// Generation is an I/O-free callback for StgGateSource and Sender. Returning
// zero fences sends immediately on Release, expiry, or a local policy change.
func (h *WindowHolder) Generation() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.active || !h.held.Live(h.cfg.Clock.Now()) || !h.cfg.Policy().enabled(h.cfg.Clock.Now()) {
		return 0
	}
	return h.held.Generation
}

// Observe is the slow, pre-transaction window read for StgGateSource. It also
// detects takeover before a renewal tick. It never adopts a newer version.
func (h *WindowHolder) Observe(ctx context.Context) (store.StgLeaseObservation, error) {
	h.io.Lock()
	defer h.io.Unlock()
	if h.Generation() == 0 {
		return store.StgLeaseObservation{}, ErrWindowWait
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	w, err := h.cfg.Windows.ReadSTGWindow(ctx)
	h.mu.Lock()
	held := h.held
	h.mu.Unlock()
	if err == nil && (w.ResourceVersion != held.ResourceVersion || w.Generation != held.Generation || w.Who != h.who || w.What != "bifrost-platform" || w.Env != "stg" || !w.Live(h.cfg.Clock.Now())) {
		err = &delivery.WindowChanged{}
	}
	if err != nil {
		h.lose(err)
		return store.StgLeaseObservation{}, err
	}
	if h.Generation() == 0 {
		return store.StgLeaseObservation{}, ErrWindowWait
	}
	return store.StgLeaseObservation{Who: w.Who, What: w.What, Env: w.Env, ResourceVersion: w.ResourceVersion, Generation: w.Generation, Expires: w.Expires}, nil
}

func (h *WindowHolder) lose(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.lost {
		return
	}
	h.active, h.lost = false, true
	h.losses <- WindowLoss{Batch: h.batch, Try: h.try, Generation: h.held.Generation, Err: err}
	h.cancel()
}

func (h *WindowHolder) renew(ctx context.Context, ticker clock.Ticker) {
	defer close(h.renewDone)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
		}
		if !h.cfg.Policy().enabled(h.cfg.Clock.Now()) {
			return
		}
		if !h.renewOnce(ctx) {
			return
		}
	}
}

func (h *WindowHolder) renewOnce(ctx context.Context) bool {
	h.io.Lock()
	defer h.io.Unlock()
	if ctx.Err() != nil || !h.cfg.Policy().enabled(h.cfg.Clock.Now()) {
		return false
	}
	h.mu.Lock()
	held, active := h.held, h.active
	h.mu.Unlock()
	if !active {
		return false
	}
	if !held.Live(h.cfg.Clock.Now()) {
		h.lose(&delivery.WindowChanged{})
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	next, err := h.cfg.Windows.RenewSTGWindow(ctx, held)
	cancel()
	if err != nil {
		// Even cancellation may hide a successful update. Do not release the
		// stale version or retry; leave its bounded TTL to expire.
		h.lose(err)
		return false
	}
	h.mu.Lock()
	h.held = next
	h.mu.Unlock()
	return h.cfg.Policy().enabled(h.cfg.Clock.Now())
}

func (h *WindowHolder) finish(ctx context.Context) {
	<-h.renewDone // the renewer has exited before any release write
	h.cancel()
	h.io.Lock()
	h.mu.Lock()
	h.active = false
	held, lost := h.held, h.lost
	h.mu.Unlock()
	var err error
	if !lost {
		releaseCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err = h.cfg.Windows.EndSTGWindow(releaseCtx, held)
		cancel()
	}
	h.mu.Lock()
	h.releaseErr = err
	h.mu.Unlock()
	h.io.Unlock()
	close(h.losses)
	close(h.done)
}

// Release cancels and waits for the renewer, then waits for its one CAS expiry
// update. A conflict is returned without retrying or touching a newer holder.
func (h *WindowHolder) Release() error {
	h.mu.Lock()
	h.active = false
	h.mu.Unlock()
	h.cancel()
	<-h.done
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.releaseErr
}
