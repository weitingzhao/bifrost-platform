package server

import (
	"context"
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	busalerts "github.com/weitingzhao/bifrost-platform/api/internal/bus/alerts"
	buscards "github.com/weitingzhao/bifrost-platform/api/internal/bus/cards"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/subscribe"
	"github.com/weitingzhao/bifrost-platform/api/internal/opsagent"
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
	"k8s.io/client-go/dynamic"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/gate"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/gatecluster"
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
// old changes behaviour. Step 2a: with BUS_NATS_URL the workers also run
// gate-cluster (ci.run.started, ci.run.finished, release.window.changed).

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
	// finished names the runs gate-cluster said finished, for the pipeline
	// records (bus step 2); recordsLive: gate-cluster watches the runs, so
	// the records' loop is only the reconciliation.
	finished     chan string
	finishedOnce sync.Once
	recordsLive  atomic.Bool
}

// finishedRuns is the queue of runs gate-cluster said finished.
func (b *busWiring) finishedRuns() chan string {
	b.finishedOnce.Do(func() { b.finished = make(chan string, 256) })
	return b.finished
}

// runFinished is gate-cluster's OnFinished: it queues the run and returns at
// once; a run that finds the queue full is left to the reconciliation, which
// counts it as missed.
func (b *busWiring) runFinished(run, _ string) {
	select {
	case b.finishedRuns() <- run:
	default:
	}
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
	// hotUntil: after a bus kick the loop keeps its 30-second cadence for a
	// while, because the kick often comes before main's CI has its verdict
	// (2026-11-16 21:06: woken 2 s after the landing, "no verdict yet", the
	// verdict at 21:06:53, the next pass only at 21:11:21).
	hotUntil time.Time
}

// headReleaseReconcileEvery is the loop's pass interval while the bus wakes
// head-release (2026-11-16: main to STG release went from 51 s to 11 s on the
// bus; the 30-second poll is kept only as the reconciliation).
const headReleaseReconcileEvery = 5 * time.Minute

// headReleaseHotAfterKick is how long the loop keeps its 30-second cadence
// after a bus kick.
const headReleaseHotAfterKick = 10 * time.Minute

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
		now := p.clock()
		recent := !p.last.IsZero() && now.Sub(p.last) < headReleaseReconcileEvery && !now.Before(p.hotUntil)
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
			p.mu.Lock()
			p.hotUntil = p.clock().Add(headReleaseHotAfterKick)
			p.mu.Unlock()
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

// announceCards writes the approval cards' transitions to the bus
// (bus/cards), in the background: the approvals flow does not wait on the
// ledger. A failed write is logged; the store stays the truth.
func (s *Server) announceCards(ts []approvals.Transition) {
	db := s.bus.db()
	if db == nil {
		return
	}
	list := make([]buscards.Card, 0, len(ts))
	for _, t := range ts {
		c := buscards.Card{Type: buscards.TypeOf(t.AuditAction), ID: t.ID, Action: t.Action, Status: t.Status, Number: t.Number}
		if buscards.Valid(c) {
			list = append(list, c)
		}
	}
	if len(list) == 0 {
		return
	}
	safego.Go("bus.cards", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := buscards.Announce(ctx, db, list); err != nil {
			slog.Warn("bus: approval cards not announced", "error", err, "count", len(list))
		}
	})
}

// announceAlerts writes Alertmanager's alerts to the bus as alert.rule.fired
// (bus/alerts). Alerts it cannot name are left out; nothing is written while
// the ledger is not connected.
func (s *Server) announceAlerts(ctx context.Context, facts []opsagent.AlertFact) error {
	db := s.bus.db()
	if db == nil {
		return nil
	}
	list := make([]busalerts.Alert, 0, len(facts))
	for _, f := range facts {
		if a, ok := busalerts.Normalize(f.Name, f.Namespace, f.Severity, f.Status); ok {
			list = append(list, a)
		}
	}
	return busalerts.Announce(ctx, db, list)
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
	if s.opsagent != nil {
		s.opsagent.Announce = s.announceAlerts
	}
	if s.approvals != nil {
		s.approvals.Announce = s.announceCards
	}
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
		if natsOn && !role.RunsWorkers() {
			s.startRunCache(ctx)
		}
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
	s.startGateCluster(ctx)
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
		s.consumeSubscriptions(ctx, js, env, pool)
		r := &relay.Relay{Ledger: ledger, Out: &relay.JSOut{JS: js, Stream: relay.EventStreamName(relay.Tenant, env)},
			Tenant: rocket, Env: env, Logf: busLogf}
		r.Run(ctx, relay.Listen(ctx, pool, busLogf))
	})
}

// startGateCluster runs gate-cluster (bus step 2a): it watches the pipelines
// namespace's runs and the release window and writes ci.run.started,
// ci.run.finished and release.window.changed into the ledger. Workers only,
// with the relay's switch (BUS_NATS_URL); without a cluster it stays off.
func (s *Server) startGateCluster(ctx context.Context) {
	if s.cluster == nil {
		return
	}
	ns := "cicd"
	if s.delivery != nil {
		ns = s.delivery.PipelinesNamespace()
	}
	g := &gatecluster.Gate{Ledger: &gatecluster.PGLedger{DB: s.bus.db}, Namespace: ns, Logf: busLogf, OnFinished: s.bus.runFinished,
		Dynamic: s.clusterDynamic}
	if s.delivery != nil {
		s.delivery.SetRunCache(g)
	}
	safego.Go("bus-gate-cluster", func() { g.Run(ctx) })
	safego.Go("bus-gate-cluster-records-live", func() {
		select {
		case <-ctx.Done():
			return
		case <-g.Watching():
		}
		s.bus.recordsLive.Store(true)
		context.AfterFunc(ctx, func() { s.bus.recordsLive.Store(false) })
		slog.Info("bus: pipeline records woken by ci.run.finished; their 30-second loop now reconciles every 5 minutes")
	})
	slog.Info("bus gate-cluster: watching PipelineRuns and the release window", "namespace", ns)
}

func (s *Server) clusterDynamic() (dynamic.Interface, error) {
	cfg, _, err := s.cluster.Service().RestConfig()
	if err != nil {
		return nil, err
	}
	return dynamic.NewForConfig(cfg)
}

// startRunCache runs, in an api process without workers, a cache-only
// gate-cluster (bus step 2 batch 2): one watch of the runs whose map the
// display reads of runs read (delivery/run_cache.go). It writes nothing to
// the ledger; appending stays in the workers' gate, which is the cache where
// both roles share a process. Same switch as the gate (BUS_NATS_URL).
func (s *Server) startRunCache(ctx context.Context) {
	if s.cluster == nil || s.delivery == nil {
		return
	}
	ns := s.delivery.PipelinesNamespace()
	g := &gatecluster.Gate{CacheOnly: true, Namespace: ns, Logf: busLogf, Dynamic: s.clusterDynamic}
	s.delivery.SetRunCache(g)
	safego.Go("bus-run-cache", func() { g.Run(ctx) })
	slog.Info("bus run cache: watching PipelineRuns for the display reads", "namespace", ns)
}

// consumeSubscriptions runs the subscription tables' dispatcher (bus/subscribe):
// one durable consumer per row the dispatcher delivers, each event handed to
// its row, a receipt left under the row's name. Rows other code still
// delivers (by: ...) are not consumed here.
func (s *Server) consumeSubscriptions(ctx context.Context, js jetstream.JetStream, env string, pool *pgxpool.Pool) {
	subs, err := registry.LoadSubscriptions()
	if err != nil {
		slog.Error("bus: subscription tables do not load; no row is dispatched", "error", err)
		return
	}
	var read subscribe.CommitMessage
	if s.delivery != nil {
		read = s.delivery.PolicyFacts().CommitMessage
	}
	send := func(ctx context.Context, key, to, body string) error {
		return store.PostConclusion(ctx, pool, key, to, body)
	}
	receipts := &relay.Receipts{JS: js, Tenant: relay.Tenant, Env: env}
	want := subscribe.Consumers(subs, env)
	// A row or a tenant that left takes its consumer with it.
	lctx, lcancel := context.WithTimeout(ctx, 10*time.Second)
	if names, err := relay.DurableNames(lctx, js, relay.Tenant, env); err != nil {
		slog.Warn("bus: subscription consumers not listed; none removed", "error", err)
	} else {
		for _, n := range subscribe.Stale(names, want, relay.Tenant) {
			if err := relay.DeleteDurable(lctx, js, relay.Tenant, env, n); err != nil {
				slog.Warn("bus: stale subscription consumer not removed", "consumer", n, "error", err)
				continue
			}
			slog.Info("bus: subscription consumer removed, its row is gone", "consumer", n)
		}
	}
	lcancel()
	for _, c := range want {
		row, name := c.Row, c.Name(relay.Tenant)
		cc, err := relay.ConsumeDurable(ctx, js, relay.Tenant, env, name, row.On, func(e relay.Envelope) error {
			dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			if err := subscribe.Deliver(dctx, row, subscribe.FromEnvelope(e), read, send); err != nil {
				slog.Warn("bus: subscription delivery; asked again", "tenant", c.Tenant, "row", row.ID, "event", e.ID, "error", err)
				return err
			}
			if err := receipts.Publish(dctx, e.ID, name); err != nil {
				slog.Warn("bus: subscription receipt", "row", row.ID, "event", e.ID, "error", err)
			}
			return nil
		})
		if err != nil {
			slog.Error("bus: subscription consumer", "tenant", c.Tenant, "row", row.ID, "error", err)
			continue
		}
		context.AfterFunc(ctx, func() { cc.Stop() })
		slog.Info("bus: subscription dispatched", "tenant", c.Tenant, "row", row.ID, "consumer", name, "on", row.On, "deliver", row.Deliver)
	}
}

// consumeHeadRelease is head-release's durable consumer: each
// git.main.advanced starts its pass now (the 30-second loop stays the
// reconciliation), is acknowledged, and leaves a receipt.
func (s *Server) consumeHeadRelease(ctx context.Context, js jetstream.JetStream, env string) {
	receipts := &relay.Receipts{JS: js, Tenant: relay.Tenant, Env: env}
	safego.Go("bus-head-release-kicks", func() { s.bus.runKicks(ctx) })
	// git.main.advanced, and ci.run.finished: main's CI verdict often comes after
	// the push that woke it (2026-11-16 21:06: the verdict 40 s after the wake).
	cc, err := relay.ConsumeDurable(ctx, js, relay.Tenant, env, "headrelease", []string{bushook.Type, gatecluster.TypeRunFinished}, func(e relay.Envelope) error {
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
