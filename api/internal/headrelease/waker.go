package headrelease

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/gatecluster"
	bushook "github.com/weitingzhao/bifrost-platform/api/internal/bus/hook"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/relay"
	"github.com/weitingzhao/bifrost-platform/api/internal/kubemetrics"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// Waker is how the bus starts head-release's pass between the controller
// loop's passes, and which event a run started by it names as its cause. The
// zero value is ready.
type Waker struct {
	head atomic.Pointer[serialPass]
	kick chan struct{}
	once sync.Once
	// wake is the bus id of the newest event worth a head-release pass
	// ("ev-<n>"): a run head-release starts names it as its cause
	// (delivery.LabelCause, 2026-11-17).
	wake atomic.Value
	// wakeEnv is the environment whose ledger those ids are of (the
	// consumer's AGENTRT_ENV).
	wakeEnv atomic.Value
}

// noteWake keeps id as the newest event worth a head-release pass.
func (b *Waker) noteWake(id string) { b.wake.Store(id) }

// LastWake is the newest event worth a head-release pass, "" for none.
func (b *Waker) LastWake() string {
	id, _ := b.wake.Load().(string)
	return id
}

// ClearWake forgets id once a run started with it as the cause; a newer wake
// that came meanwhile stays.
func (b *Waker) ClearWake(id string) { b.wake.CompareAndSwap(id, "") }

// WakeEnv is the environment of the wakes' ledger, "" before the first.
func (b *Waker) WakeEnv() string {
	env, _ := b.wakeEnv.Load().(string)
	return env
}

// serialPass runs a pass one at a time, whoever asks: the controller's
// 30-second loop and the bus's kick.
type serialPass struct {
	mu   sync.Mutex
	pass func(context.Context) error
	last time.Time
	now  func() time.Time
	// hotUntil: after a bus kick the loop keeps its 30-second cadence for a
	// while, because the kick often comes before main's CI has its verdict
	// (2026-11-16 21:06: woken 2 s after the landing, "no verdict yet", the
	// verdict at 21:06:53, the next pass only at 21:11:21).
	hotUntil time.Time
}

// reconcileEvery is the loop's pass interval: the bus wakes head-release
// (2026-11-16: main to STG release went from 51 s to 11 s), and polling stays
// only as the reconciliation (bus/RULES.md §8). With the bus down a release
// waits for this pass (2026-11-18, L04: the 30-second mode for that case is
// gone).
const reconcileEvery = 5 * time.Minute

// hotAfterKick is how long the loop keeps its 30-second cadence after a bus
// kick.
const hotAfterKick = 10 * time.Minute

func (p *serialPass) clock() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}

func (p *serialPass) run(ctx context.Context) error {
	ctx = kubemetrics.WithSite(ctx, "head-release")
	p.mu.Lock()
	defer p.mu.Unlock()
	p.last = p.clock()
	return p.pass(ctx)
}

// tick is the controller loop's call: a pass once per reconcileEvery, or at
// each tick for a while after a bus kick.
func (p *serialPass) tick(ctx context.Context) error {
	p.mu.Lock()
	now := p.clock()
	recent := !p.last.IsZero() && now.Sub(p.last) < reconcileEvery && !now.Before(p.hotUntil)
	p.mu.Unlock()
	if recent {
		return nil
	}
	return p.run(ctx)
}

// Serial wraps head-release's pass so the bus can start it between the loop's
// passes; nil stays nil (the switch is off).
func (b *Waker) Serial(pass func(context.Context) error) func(context.Context) error {
	if pass == nil {
		return nil
	}
	p := &serialPass{pass: pass}
	b.head.Store(p)
	return p.tick
}

func (b *Waker) kicks() chan struct{} {
	b.once.Do(func() { b.kick = make(chan struct{}, 1) })
	return b.kick
}

// kickPass asks for a head-release pass now; asks that come while one waits
// are folded into it.
func (b *Waker) kickPass() {
	select {
	case b.kicks() <- struct{}{}:
	default:
	}
}

func (b *Waker) runKicks(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-b.kicks():
		}
		if p := b.head.Load(); p != nil {
			p.mu.Lock()
			p.hotUntil = p.clock().Add(hotAfterKick)
			p.mu.Unlock()
			if err := p.run(ctx); err != nil && ctx.Err() == nil {
				slog.Warn("bus: head-release pass started by git.main.advanced", "error", err)
			}
		}
	}
}

// wakeTypes are the events that start a head-release pass now.
var wakeTypes = []string{bushook.Type, gatecluster.TypeRunFinished, gatecluster.TypeWindowChanged}

// Consume is head-release's durable consumer: each git.main.advanced starts
// its pass now (the 30-second loop stays the reconciliation), is acknowledged,
// and leaves a receipt.
func (b *Waker) Consume(ctx context.Context, js jetstream.JetStream, env string) {
	receipts := &relay.Receipts{JS: js, Tenant: relay.Tenant, Env: env}
	safego.Go("bus-head-release-kicks", func() { b.runKicks(ctx) })
	// git.main.advanced, and ci.run.finished: main's CI verdict often comes after
	// the push that woke it (2026-11-16 21:06: the verdict 40 s after the wake).
	// And release.window.changed: a pass that found the window held waited
	// for the next 5-minute reconciliation; the window's release now wakes it
	// (2026-11-16 night: of 564 s from main to STG released, 267 s were
	// waiting for the window).
	// Only the events of the enabled rows start a pass (Wakes, 2026-11-17:
	// every run of every pipeline and every window renewal woke it, ~400 LISTs
	// per 10 minutes). Without the table, every event does.
	var worth func(relay.Envelope) bool
	if table, _, err := LoadTable(); err == nil {
		w := WakesOf(table)
		worth = func(e relay.Envelope) bool { return w.Worth(e.Type, e.Subject, e.Change) }
	}
	cc, err := relay.ConsumeDurable(ctx, js, relay.Tenant, env, "headrelease", wakeTypes, func(e relay.Envelope) error {
		if worth == nil || worth(e) {
			b.wakeEnv.Store(env)
			b.noteWake(e.ID)
			b.kickPass()
		}
		rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := receipts.Publish(rctx, e.ID, "headrelease"); err != nil {
			slog.Warn("bus: head-release receipt", "id", e.ID, "error", err)
		}
		return nil
	})
	if err != nil {
		slog.Error("bus: head-release consumer; its 5-minute pass alone starts releases", "error", err)
		return
	}
	context.AfterFunc(ctx, func() { cc.Stop() })
	slog.Info("bus: head-release woken by git.main.advanced")
}
