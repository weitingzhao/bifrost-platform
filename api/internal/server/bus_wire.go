package server

import (
	"context"
	"errors"
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/alertletters"
	busalerts "github.com/weitingzhao/bifrost-platform/api/internal/bus/alerts"
	buscards "github.com/weitingzhao/bifrost-platform/api/internal/bus/cards"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/runhooks"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/subscribe"
	"github.com/weitingzhao/bifrost-platform/api/internal/headrelease"
	"github.com/weitingzhao/bifrost-platform/api/internal/kubemetrics"
	"github.com/weitingzhao/bifrost-platform/api/internal/opsagent"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitwatch"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"k8s.io/client-go/dynamic"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/drill"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/gate"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/gatecluster"
	bushook "github.com/weitingzhao/bifrost-platform/api/internal/bus/hook"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/mailwake"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/ownerq"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/relay"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/states"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitexec"
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
// What each part does lives in its bus package; this file starts them.

// busWiring is what the bus is in this process.
type busWiring struct {
	pool atomic.Pointer[pgxpool.Pool]
	gate atomic.Pointer[gate.Gate]
	// head is how the bus starts head-release's pass and which event a started
	// run names as its cause.
	head headrelease.Waker
	// finished names the runs gate-cluster said finished, for the pipeline
	// records (bus step 2).
	finished gatecluster.FinishedRuns
	// gitLedger: this environment keeps git.main.advanced in the ledger
	// (the Gitea hook or the bus is on), so display readers may ask it where
	// main is (mainOf).
	gitLedger atomic.Bool
	// runHooks are the readers woken by ci.run.finished instead of a timer
	// (bus/runhooks); RunHooks registers one.
	runHooks runhooks.Registry
	// mail wakes the seats waiting on their inbox (bus/mailwake); mailLive:
	// this process follows seat.mail.arrived.
	mail     mailwake.Hub
	mailLive atomic.Bool
}

func (b *busWiring) db() eventlog.DB {
	if p := b.pool.Load(); p != nil {
		return p
	}
	return nil
}

// announceCards writes the approval cards' transitions to the bus (bus/cards),
// in the background: the approvals flow does not wait on the ledger.
func (s *Server) announceCards(ts []approvals.Transition) {
	db := s.bus.db()
	if db == nil {
		return
	}
	list := make([]buscards.Card, 0, len(ts))
	for _, t := range ts {
		c := buscards.Card{Type: buscards.TypeOf(t.AuditAction), ID: t.ID, Action: t.Action, Status: t.Status, Number: t.Number}
		if c.Type == buscards.TypeNotified {
			c.Kind, c.Channel = buscards.ParseNotice(t.Detail)
		}
		list = append(list, c)
	}
	buscards.AnnounceInBackground(db, list)
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
			a.Host = busalerts.HostOf(f.Host, f.Node)
			list = append(list, a)
		}
	}
	fired, err := busalerts.Announce(ctx, db, list)
	if err != nil || len(fired) == 0 {
		return err
	}
	// Rows marked by: webhook are delivered here, without the bus (TD-588):
	// an alarm about the bus must reach its seat while the bus is down.
	subs, serr := registry.LoadSubscriptions()
	if serr != nil {
		slog.Warn("bus: direct alert delivery: the subscription table does not load", "error", serr)
		return nil
	}
	d := subscribe.Dispatcher{DB: db, Send: func(ctx context.Context, key, to, body string, cause int64) error {
		return coord.PostConclusionCaused(ctx, db, key, to, body, cause)
	}}
	for _, f := range fired {
		ev := subscribe.Event{ID: relay.EventID(f.EventID), Type: busalerts.Type, Subject: busalerts.SubjectOf(f.Alert),
			Ref: busalerts.Ref, Change: f.Status}
		d.DeliverDirect(ctx, subs, relay.Tenant, ev)
	}
	return nil
}

func busLogf(format string, args ...any) { slog.Info(fmt.Sprintf(format, args...)) }

// busEventsAfter is the event log's cross-stream read, for the gate and the
// observer. Drill streams are left out: nobody follows a drill and it does not
// count as an event nobody heard (bus/drill). relayEventsAfter is the same read
// for the relay, drills included: a drill goes through the relay to the stream,
// which is what it proves.
var (
	busEventsAfter   = relay.EventsAfterOf(eventlog.EventFilter{HiddenPrefixes: []string{drill.StreamPrefix}})
	relayEventsAfter = relay.EventsAfterOf(eventlog.EventFilter{})
)

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
		s.announceQuestionsOnBus()
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
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/bus/clients", route((*gate.Gate).HandleClients))
	// The problem loop, computed when asked (agentrt/coord, OWNER_LAW #36).
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/bus/problems", s.handleBusProblems)
	// Each subject's latest state for a registered state type (2026-11-20).
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/bus/states", states.Deps{DB: s.bus.db}.Handle)
	// What the bus's work does, computed when asked (bus/effect).
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/bus/effect", s.handleBusEffect)
	// The known regressions on main (bus/regressions): a seat records one or its fix as itself.
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/bus/regressions", s.busRegressions().HandleList)
	writers := s.auth.RequireAny(actuation.RoleReporter, actuation.RoleOperator, actuation.RoleAdmin)
	r.With(writers).Post("/bus/regressions", s.guard("report_regression", s.handleRegressionWrite(false)))
	r.With(writers).Post("/bus/regressions/fixed", s.guard("mark_regression_fixed", s.handleRegressionWrite(true)))
	// The Owner's uses of privilege (bus/privilege): the coordinator records each as itself.
	r.With(writers).Post("/bus/privilege", s.guard("record_privilege_use", s.busPrivilege().HandleRecord))
	// Foundation's readings (2026-11-21): the decoupling acceptance and the agent guard's refusals.
	r.With(writers).Post("/bus/acceptance", s.guard("record_acceptance_reading", s.busAcceptance().HandleRecord))
	r.With(writers).Post("/bus/guard-blocks", s.guard("record_guard_block", s.busGuardBlocks().HandleRecord))
	// The change of generation, from its registers (bus/effect).
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/bus/nextgen", s.handleBusNextgen)
	// What the bus promises and to whom, from its registers (bus/registry).
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/bus/contract", registry.HandleContract)
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/bus/observe", s.busObserve().ServeHTTP)
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
	s.bus.gitLedger.Store((hookOn || natsOn) && s.delivery != nil)

	if role.RunsAPI() {
		g := &gate.Gate{Ledger: ledger, Tenant: rocket, Env: env, Wake: &gate.Waker{}, Logf: busLogf, Since: relay.CursorAt(pool)}
		if subs, err := registry.LoadSubscriptions(); err == nil {
			g.Clients = subscribe.RosterClients(subs, env)
			g.TablesLoadedAt = time.Now()
		}
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
					receipts := &relay.Receipts{JS: js, Tenant: relay.Tenant, Env: env}
					g.Receipts = receipts
					journey := &gate.JSJourney{JS: js, Tenant: relay.Tenant, Env: env}
					g.Journey = journey
					safego.Go("bus-receipts-warm", func() { journey.Warm(ctx) })
					g.ReceiptFeed = &gate.ReceiptFeed{}
					safego.Go("bus-gate-follow-receipts", func() { gate.FollowReceipts(ctx, js, relay.Tenant, env, g.ReceiptFeed, busLogf) })
					s.followSeatMail(ctx, nc, receipts, env)
				}
				context.AfterFunc(ctx, nc.Close)
			}
		}
		// One follower per process wakes every waiter; waiters no longer
		// re-read on their own timers (bus step 4, 2026-11-19).
		safego.Go("bus-gate-follower", func() { g.Follow(ctx) })
		s.bus.gate.Store(g)
		slog.Info("bus gate: on", "env", env, "nats", natsOn)
		if natsOn && !role.RunsWorkers() {
			s.startRunCache(ctx)
		}
	}

	if !role.RunsWorkers() {
		return
	}
	busalerts.StartRelayWatch(ctx, s.bus.db, s.promPresent)
	coord.StartAskWatch(ctx, s.bus.db)
	s.startBusDrill(ctx)
	if (hookOn || natsOn) && s.delivery != nil {
		rec := &bushook.Reconciler{DB: s.bus.db, Repos: bushook.ReposFromEnv(), Head: s.agentRuntimeMains(), Logf: busLogf}
		safego.Go("bus-gate-git-reconcile", func() { rec.Run(ctx) })
		slog.Info("bus gate-git: reconciling main every 5 minutes", "repos", rec.Repos)
	}
	if !natsOn {
		return
	}
	s.startGateCluster(ctx, env)
	s.startRunSummaries(ctx)
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
		if !relay.EnsureEventStreamUntil(ctx, js, relay.Tenant, env) {
			return
		}
		safego.Go("bus-stream-sizes", func() { relay.WatchStreamSizes(ctx, js, relay.Tenant, env) })
		if headrelease.On() || headrelease.ProdShadowOn() {
			s.bus.head.Consume(ctx, js, env)
		}
		s.subscriptions(pool).Run(ctx, js, env)
		s.bus.runHooks.Consume(ctx, js, env, gatecluster.TypeRunFinished)
		s.consumeConclusions(ctx, js, env)
		s.consumeWindowQueue(ctx, js, env)
		alertletters.Consumer{DB: s.bus.db, Post: coord.PostConclusionCaused}.Consume(ctx, js, env)
		s.consumeUnitWatch(ctx, js, env)
		s.consumeReleaseRecords(ctx, js, env)
		r := &relay.Relay{Ledger: &relay.PGLedger{DB: func() relay.TxBeginner { return pool }, EventsAfter: relayEventsAfter}, Out: &relay.JSOut{JS: js, Stream: relay.EventStreamName(relay.Tenant, env)},
			Tenant: rocket, Env: env, Logf: busLogf}
		r.Run(ctx, relay.Listen(ctx, pool, busLogf))
	})
}

// startGateCluster runs gate-cluster (bus step 2a): it watches the pipelines
// namespace's runs and the release window and writes ci.run.started,
// ci.run.finished and release.window.changed into the ledger. Workers only,
// with the relay's switch (BUS_NATS_URL); without a cluster it stays off.
func (s *Server) startGateCluster(ctx context.Context, env string) {
	if s.cluster == nil {
		return
	}
	ns := "cicd"
	if s.delivery != nil {
		ns = s.delivery.PipelinesNamespace()
	}
	g := &gatecluster.Gate{Ledger: &gatecluster.PGLedger{DB: s.bus.db}, Namespace: ns, Env: env, Logf: busLogf, OnFinished: s.bus.finished.Add,
		UnitTaskLabel: unitexec.LabelPlan, Dynamic: s.clusterDynamic, Watches: watchSpecs("rocket")}
	if s.delivery != nil {
		s.delivery.SetRunCache(g, s.earnedReadRoutes(ctx))
	}
	safego.Go("bus-gate-cluster", func() { g.Run(kubemetrics.WithSite(ctx, "bus.gate-cluster")) })
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
	g := &gatecluster.Gate{CacheOnly: true, WatchTasks: true, UnitTaskLabel: unitexec.LabelPlan, Namespace: ns, Logf: busLogf, Dynamic: s.clusterDynamic}
	s.delivery.SetRunCache(g, s.earnedReadRoutes(ctx))
	safego.Go("bus-run-cache", func() { g.Run(kubemetrics.WithSite(ctx, "bus.run-cache")) })
	slog.Info("bus run cache: watching PipelineRuns for the display reads", "namespace", ns)
}

// followSeatMail wakes the inbox waiters of this api process on
// seat.mail.arrived (mailwake.Hub.Follow).
func (s *Server) followSeatMail(ctx context.Context, nc *nats.Conn, receipts *relay.Receipts, env string) {
	if err := s.bus.mail.Follow(ctx, nc, receipts, relay.EventSubject(relay.Tenant, env, coord.SeatMailType)); err != nil {
		slog.Error("bus: seat mail subscription; inbox waiters only re-read", "error", err)
		return
	}
	s.bus.mailLive.Store(true)
	slog.Info("bus: inbox waiters woken by seat.mail.arrived", "env", env)
}

// mailWake is the inbox read's wake (agentrtAPI.MailWake): none until this
// process follows seat.mail.arrived.
func (s *Server) mailWake(addr string) (<-chan struct{}, func()) {
	if !s.bus.mailLive.Load() {
		return nil, func() {}
	}
	return s.bus.mail.Listen(addr)
}

// subscriptions is the subscription tables' dispatcher as this process wires it.
func (s *Server) subscriptions(pool *pgxpool.Pool) subscribe.Dispatcher {
	d := subscribe.Dispatcher{DB: pool, Detail: s.busRunDetail(),
		Send: func(ctx context.Context, key, to, body string, cause int64) error {
			return coord.PostConclusionCaused(ctx, pool, key, to, body, cause)
		}}
	if s.delivery != nil {
		d.Read = s.delivery.PolicyFacts().CommitMessage
	}
	return d
}

// consumeConclusions delivers the approvals' and questions' conclusions to the
// seat that asked as soon as the bus says one changed (2026-11-18; N10 itself,
// no copy at all, is still ahead; the 15-second loops reconcile). Each pass is the same scan
// the loop runs: a conclusion is posted once, under its own key, whoever finds
// it first.
func (s *Server) consumeConclusions(ctx context.Context, js jetstream.JetStream, env string) {
	if s.approvals != nil {
		relay.ConsumePasses(ctx, js, env, "card-created-notice", []string{buscards.TypeCreated}, s.approvals.NotifyCreated)
	}
	relay.ConsumePasses(ctx, js, env, "card-conclusions", []string{buscards.TypeDecided, buscards.TypeExecuted}, s.cardConclusionsPass)
	relay.ConsumePasses(ctx, js, env, "question-conclusions", []string{ownerq.TypeAnswered}, s.questionConclusionsPass)
}

// consumeWindowQueue moves the release window's queue on as soon as the window
// changes (L23, N11, 2026-11-18): the next in line hears at once that it is its
// turn. The periodic advance stays, for a hold that simply runs out.
func (s *Server) consumeWindowQueue(ctx context.Context, js jetstream.JetStream, env string) {
	if s.delivery == nil {
		return
	}
	relay.ConsumePasses(ctx, js, env, "window-queue", []string{gatecluster.TypeWindowChanged}, func(context.Context) {
		if err := s.delivery.AdvanceWindowQueue(); err != nil {
			slog.Warn("bus: window queue not advanced", "error", err)
		}
	})
}

// consumeUnitWatch reads main's unit declaration when main moves (unitwatch).
func (s *Server) consumeUnitWatch(ctx context.Context, js jetstream.JetStream, env string) {
	if s.delivery == nil {
		return
	}
	unitwatch.OnMainMoved(ctx, env, s.unitWatchPass, func(name string, each func(context.Context)) {
		relay.ConsumePasses(ctx, js, env, name, []string{bushook.Type}, each)
	})
}

func (s *Server) unitWatchPass(ctx context.Context) error {
	db := s.bus.db()
	if db == nil {
		return errors.New("the ledger is not connected")
	}
	return unitwatch.Watch{DB: db, Head: s.giteaMainHead, Tree: s.unitTreesOf(), FirstParent: s.giteaFirstParent}.Pass(ctx)
}

// consumeReleaseRecords asks the release recorder for a pass when a run ends
// (N21, 2026-11-18; it was told in-process by the pipeline records' loop).
func (s *Server) consumeReleaseRecords(ctx context.Context, js jetstream.JetStream, env string) {
	if s.releases == nil || s.releases.Service() == nil {
		return
	}
	relay.ConsumePasses(ctx, js, env, "release-recorder", []string{gatecluster.TypeRunFinished}, func(context.Context) {
		s.releases.Service().Kick()
	})
}

func (s *Server) cardConclusionsPass(ctx context.Context) {
	if s.approvals != nil {
		s.approvals.RetryThreadInbox(ctx)
	}
}

func (s *Server) questionConclusionsPass(ctx context.Context) {
	if s.questionRuntime != nil && s.questionRuntime.Svc != nil {
		s.questionRuntime.Svc.RetryThreadInbox(ctx)
	}
}

// watchSpecs are a tenant's watch rows (bus/registry/watches) with the ref
// of each row's topic: what gate-cluster says of the cluster's objects. A
// row that does not parse leaves the gate without watches and says so; the
// register's tests refuse such a file before it ships.
func watchSpecs(tenant string) []gatecluster.WatchSpec {
	rows, err := registry.Watches()
	if err != nil {
		busLogf("bus gate-cluster: no watches: %v", err)
		return nil
	}
	tenants, err := registry.Load()
	if err != nil {
		busLogf("bus gate-cluster: no watches: %v", err)
		return nil
	}
	out := make([]gatecluster.WatchSpec, 0, len(rows[tenant]))
	for _, w := range rows[tenant] {
		ch := tenants[tenant].Channels[w.Type]
		out = append(out, gatecluster.WatchSpec{Watch: w, Ref: ch.Ref, Window: states.Window(ch.Retention)})
	}
	return out
}
