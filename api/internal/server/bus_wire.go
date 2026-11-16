package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/gate"
	bushook "github.com/weitingzhao/bifrost-platform/api/internal/bus/hook"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/relay"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// The bus, step 1 (api/internal/bus/RULES.md): the ledger is agentrt's event
// log; gate-git writes git.main.advanced into it (Gitea's hook and a 5-minute
// reconciliation); in workers the relay publishes the ledger to NATS and
// head-release holds a durable consumer that starts its pass at once; in the
// api process the gate serves the ledger's events to long polls and SSE,
// woken by NATS. Everything is off unless BUS_NATS_URL (relay, wake,
// consumer) or BUS_GITEA_HOOK_SECRET (hook, reconciliation) is set; nothing
// old changes behaviour.

// busWiring is what the bus is in this process.
type busWiring struct {
	pool atomic.Pointer[pgxpool.Pool]
	gate atomic.Pointer[gate.Gate]
	head atomic.Pointer[serialPass]
	kick chan struct{}
	once sync.Once
	// consumerLive: head-release's durable consumer is subscribed, so the
	// bus wakes it and its own loop is only the reconciliation.
	consumerLive atomic.Bool
}

// serialPass runs a pass one at a time, whoever asks: the controller's
// 30-second loop and the bus's kick.
type serialPass struct {
	mu   sync.Mutex
	pass func(context.Context) error
	last time.Time
	// live says the bus wakes the pass; then the loop's tick only reconciles,
	// at most once per reconcileEvery (bus/RULES.md §8: polling stays only
	// as registered reconciliation).
	live func() bool
	now  func() time.Time
}

// headReleaseReconcileEvery is the loop's pass interval while the bus wakes
// head-release (2026-11-16: main to STG release went from 51 s to 11 s on the
// bus; the 30-second poll is kept only as the reconciliation).
const headReleaseReconcileEvery = 5 * time.Minute

func (p *serialPass) clock() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}

func (p *serialPass) run(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.last = p.clock()
	return p.pass(ctx)
}

// tick is the controller loop's call: a full pass while the bus is not
// waking head-release, else one every headReleaseReconcileEvery.
func (p *serialPass) tick(ctx context.Context) error {
	if p.live != nil && p.live() {
		p.mu.Lock()
		recent := !p.last.IsZero() && p.clock().Sub(p.last) < headReleaseReconcileEvery
		p.mu.Unlock()
		if recent {
			return nil
		}
	}
	return p.run(ctx)
}

// serialHeadRelease wraps head-release's pass so the bus can start it between
// the loop's passes; nil stays nil (the switch is off).
func (b *busWiring) serialHeadRelease(pass func(context.Context) error) func(context.Context) error {
	if pass == nil {
		return nil
	}
	p := &serialPass{pass: pass, live: b.consumerLive.Load}
	b.head.Store(p)
	return p.tick
}

func (b *busWiring) kicks() chan struct{} {
	b.once.Do(func() { b.kick = make(chan struct{}, 1) })
	return b.kick
}

// kickHeadRelease asks for a head-release pass now; asks that come while one
// waits are folded into it.
func (b *busWiring) kickHeadRelease() {
	select {
	case b.kicks() <- struct{}{}:
	default:
	}
}

func (b *busWiring) runKicks(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-b.kicks():
		}
		if p := b.head.Load(); p != nil {
			if err := p.run(ctx); err != nil && ctx.Err() == nil {
				slog.Warn("bus: head-release pass started by git.main.advanced", "error", err)
			}
		}
	}
}

func (b *busWiring) db() eventlog.DB {
	if p := b.pool.Load(); p != nil {
		return p
	}
	return nil
}

func busLogf(format string, args ...any) { slog.Info(fmt.Sprintf(format, args...)) }

// busEventsAfter is the event log's cross-stream read, for the relay and the
// gate (they import no domain package).
func busEventsAfter(ctx context.Context, tx pgx.Tx, after relay.Cursor, limit int, types []string) ([]byte, relay.Cursor, error) {
	doc, next, err := store.EventsAfter(ctx, tx, store.EventCursor{TxID: after.TxID, EventID: after.EventID}, limit, store.EventFilter{Types: types})
	return doc, relay.Cursor{TxID: next.TxID, EventID: next.EventID}, err
}

// mountBus mounts the gate's routes and Gitea's hook. The gate answers 503
// until the ledger is connected (and without AGENTRT_ENV); the hook answers
// 503 without its secret.
func (s *Server) mountBus(r chi.Router) {
	h := bushook.New(s.bus.db)
	r.Post("/bus/hooks/gitea", h.ServeHTTP)
	route := func(serve func(*gate.Gate, http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			g := s.bus.gate.Load()
			if g == nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the bus gate is not ready: the ledger is not connected"})
				return
			}
			serve(g, w, r)
		}
	}
	// Read-only, viewer and above: the events name repositories, commits and
	// seats. /bus/stream keeps the bearer too: the Console reads it with fetch
	// streaming (an EventSource cannot send a bearer).
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/bus/events", route((*gate.Gate).HandleEvents))
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/bus/stream", route((*gate.Gate).HandleStream))
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/bus/trace", route((*gate.Gate).HandleTrace))
}

// startBus starts what this process runs of the bus, once the ledger's pool
// is open; it stops with ctx.
func (s *Server) startBus(ctx context.Context, pool *pgxpool.Pool, env string, role config.Role) {
	tenants, err := registry.Load()
	if err != nil {
		slog.Error("bus: the register does not load; the bus is off", "error", err)
		return
	}
	rocket, ok := tenants[relay.Tenant]
	if !ok || !relay.EnvRE.MatchString(env) {
		slog.Info("bus: off (no rocket register, or AGENTRT_ENV is not an environment name)", "env", env)
		return
	}
	s.bus.pool.Store(pool)
	ledger := &relay.PGLedger{DB: func() relay.TxBeginner { return pool }, EventsAfter: busEventsAfter}
	cfg, natsOn, err := relay.ConfigFromEnv(env)
	if err != nil {
		slog.Error("bus: NATS is off", "error", err)
	}
	hookOn := strings.TrimSpace(os.Getenv(bushook.EnvSecret)) != ""

	if role.RunsAPI() {
		g := &gate.Gate{Ledger: ledger, Tenant: rocket, Env: env, Wake: &gate.Waker{}, Logf: busLogf}
		if natsOn {
			nc, cerr := relay.Connect(cfg.URL, relay.ReaderUser, cfg.ReaderPassword, "platform-api bus gate")
			if cerr != nil {
				slog.Error("bus gate: NATS connection; waiters re-read the ledger", "error", cerr)
			} else {
				if _, serr := gate.WakeOn(nc, relay.Tenant, env, g.Wake); serr != nil {
					slog.Error("bus gate: NATS subscription; waiters re-read the ledger", "error", serr)
				}
				g.NATSUp = nc.IsConnected
				if js, jerr := jetstream.New(nc); jerr == nil {
					g.Receipts = &relay.Receipts{JS: js, Tenant: relay.Tenant, Env: env}
					g.Journey = &gate.JSJourney{JS: js, Tenant: relay.Tenant, Env: env}
				}
				context.AfterFunc(ctx, nc.Close)
			}
		}
		s.bus.gate.Store(g)
		slog.Info("bus gate: on", "env", env, "nats", natsOn)
	}

	if !role.RunsWorkers() {
		return
	}
	if (hookOn || natsOn) && s.delivery != nil {
		rec := &bushook.Reconciler{DB: s.bus.db, Repos: bushook.ReposFromEnv(), Head: s.agentRuntimeMains(), Logf: busLogf}
		safego.Go("bus-gate-git-reconcile", func() { rec.Run(ctx) })
		slog.Info("bus gate-git: reconciling main every 5 minutes", "repos", rec.Repos)
	}
	if !natsOn {
		return
	}
	nc, err := relay.Connect(cfg.URL, relay.RelayUser, cfg.RelayPassword, "platform-workers bus relay")
	if err != nil {
		slog.Error("bus relay: NATS connection; the relay is off", "error", err)
		return
	}
	context.AfterFunc(ctx, nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		slog.Error("bus relay: JetStream; the relay is off", "error", err)
		return
	}
	safego.Go("bus-relay", func() {
		for {
			ectx, cancel := context.WithTimeout(ctx, 10*time.Second)
			_, err := relay.EnsureEventStream(ectx, js, relay.Tenant, env)
			cancel()
			if err == nil {
				break
			}
			slog.Error("bus relay: the events stream could not be ensured; again in 15s", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(15 * time.Second):
			}
		}
		if headReleaseOn() {
			s.consumeHeadRelease(ctx, js, env)
		}
		r := &relay.Relay{Ledger: ledger, Out: &relay.JSOut{JS: js, Stream: relay.EventStreamName(relay.Tenant, env)},
			Tenant: rocket, Env: env, Logf: busLogf}
		r.Run(ctx, relay.Listen(ctx, pool, busLogf))
	})
}

// consumeHeadRelease is head-release's durable consumer: each
// git.main.advanced starts its pass now (the 30-second loop stays the
// reconciliation), is acknowledged, and leaves a receipt.
func (s *Server) consumeHeadRelease(ctx context.Context, js jetstream.JetStream, env string) {
	receipts := &relay.Receipts{JS: js, Tenant: relay.Tenant, Env: env}
	safego.Go("bus-head-release-kicks", func() { s.bus.runKicks(ctx) })
	cc, err := relay.ConsumeDurable(ctx, js, relay.Tenant, env, "headrelease", bushook.Type, func(e relay.Envelope) error {
		s.bus.kickHeadRelease()
		rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := receipts.Publish(rctx, e.ID, "headrelease"); err != nil {
			slog.Warn("bus: head-release receipt", "id", e.ID, "error", err)
		}
		return nil
	})
	if err != nil {
		slog.Error("bus: head-release consumer; the 30-second loop alone starts releases", "error", err)
		return
	}
	s.bus.consumerLive.Store(true)
	context.AfterFunc(ctx, func() { s.bus.consumerLive.Store(false); cc.Stop() })
	slog.Info("bus: head-release woken by git.main.advanced; its 30-second loop now reconciles every 5 minutes")
}
