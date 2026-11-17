package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryview"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/assessmentreadings"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// agentrtDatabase is the state database of each environment (design v5
// §5.1.1); the migrate subcommand keeps the same table.
var agentrtDatabase = map[string]string{
	"prod": "bifrost_platform",
	"stg":  "bifrost_platform_stg",
	"dev":  "bifrost_platform_dev",
}

// agentRuntime is the agent runtime as this process runs it.
type agentRuntime struct {
	api  *agentrtapi.API
	stop func()
	// env is AGENTRT_ENV: whose runtime this platform runs.
	env string
	// schema is what the platform last read of the database's agentrt
	// schema (TD-411, agentrt_schema.go); nil without AGENTRT_DSN.
	schema *schemaWatch
	// evidenceCI is the ledger answering "did CI pass" (evidence_gate.go);
	// nil in a process with no delivery service: then CI is not read.
	evidenceCI delivery.CIReader
	// late is the runtime's database as it connects: its routes are ready
	// once it is (/health says agentrt_ready); nil without AGENTRT_DSN.
	late *lateDB
	// fullCI is the controller's daily full CI once the workers process
	// has wired it (wireFullCI), for the gauge /metrics serves; nil in a
	// process that runs no controller, or where the loop is off.
	fullCI atomic.Pointer[controller.FullCI]
}

// wireAgentRuntime connects the agent runtime (design v5 §5.2) when
// AGENTRT_DSN is set (a libpq connection string without the password; the
// password comes from AGENTRT_DB_PASSWORD, which the Deployment fills from
// the Secret agentrt-db-runtime): a pool, refused unless it is the runtime role of
// AGENTRT_ENV on that environment's database; the routes; and in the workers
// process the controller loops. Without it the routes answer 503 and nothing
// runs, so the code ships before the database is wired into the Deployment.
//
// The controller reads approvals, questions, CI verdicts and deployments from the
// platform's own stores (agentRuntimeSources); a record it cannot read leaves
// the rule atoms on it unknown, so nothing is judged done on facts it cannot
// see. Pushed branches are checked on Gitea in the organization
// AGENTRT_GIT_ORG (agentrt_heads.go); without it, persist and salvage of a
// branch answer 503.
func (s *Server) wireAgentRuntime(role config.Role) {
	rt := &agentRuntime{api: &agentrtapi.API{Logf: func(format string, args ...any) {
		slog.Warn("agentrt", "detail", fmt.Sprintf(format, args...))
	}, Ask: s.questionRuntime.agentRuntimeAsk(), Anchor: releasepolicy.OwnerKeyFingerprint,
		DiskFloor: diskFloorFromEnv(), PinReason: pinReasonFromEnv()}, env: strings.TrimSpace(os.Getenv("AGENTRT_ENV"))}
	s.agentRuntime = rt
	rt.evidenceCI = s.wireEvidenceGate()
	if s.releases != nil && s.releases.Service() != nil {
		rt.api.ProductionDelivery = func(ctx context.Context) deliveryview.DeliveryProduction {
			return productionDeliveryEvidence(ctx, s.releases.Service())
		}
	}
	if s.delivery != nil {
		rt.api.ConfigurationBoundaries = s.delivery.ConfigurationBoundaries
	}
	if s.approvals != nil {
		s.approvals.Assessment = s.assessmentReader(assessmentreadings.AssessApproval)
	}
	s.wireQuestionAssessments()
	dsn := strings.TrimSpace(os.Getenv("AGENTRT_DSN"))
	if dsn == "" {
		slog.Info("agentrt: AGENTRT_DSN is not set; its routes answer 503")
		return
	}
	rt.schema = &schemaWatch{}
	// The routes answer 503 until the database is connected; the platform
	// keeps connecting in the background, so a refusal at start (a new pod
	// the network policy does not know yet, a database restarting) heals by
	// itself instead of leaving the runtime off until the next restart.
	late := &lateDB{}
	rt.api.DB, rt.api.Back, rt.late = late, late.Back, late
	s.wireThreadInbox(late)
	s.wireWindowNotices(late)
	if s.delivery != nil && role.RunsWorkers() {
		s.delivery.EnableWindowQueueAdvance()
	}
	if s.delivery != nil {
		s.delivery.BindWindowRecorder(s.recordWindowEvent)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	env := strings.TrimSpace(os.Getenv("AGENTRT_ENV"))
	// STG's card-auto-answer rule answers the runtime's cards within
	// seconds: there the question store takes them past its quotas (TD-393).
	s.questionRuntime.takeCardsByRule(env == "stg")
	safego.Go("agentrt-connect", func() {
		defer close(done)
		pool := connectAgentRuntime(ctx, func(ctx context.Context) (*pgxpool.Pool, error) {
			return openAgentRuntime(ctx, dsn, env)
		}, agentrtRetry)
		if pool == nil {
			return
		}
		// The routes open, and /health says agentrt_ready, with the moment
		// they did already recorded: the API's readers count a host's silence
		// from no earlier (review 528 A1b).
		late.open(pool, time.Now())
		s.followWindowExpiry(ctx)
		// The seats the token table names that the platform registers
		// itself (store.EnsureSeats): the coordinator's proposer seat.
		if seats, err := store.EnsureSeats(ctx, pool, s.auth.Seats()); err != nil {
			slog.Error("agentrt: a configured seat could not be registered", "err", err)
		} else if len(seats) > 0 {
			slog.Info("agentrt: seats registered", "seats", seats)
		}
		// Both processes say whether the database has this binary's
		// migrations (TD-411), in their health and their log.
		safego.Go("agentrt-schema", func() { rt.schema.run(ctx, pool) })
		s.startBus(ctx, pool, env, role)
		if !role.RunsWorkers() {
			<-ctx.Done()
			pool.Close()
			return
		}
		c := agentRuntimeController(env, pool, s.agentRuntimeSources())
		// The old landing lane is retired (the Owner 2026-11-13): its loops
		// (drill, live claims, cards, grant requests, reconcile, the reading
		// of main for landed results) are not wired any more. Changes reach
		// main through ready/ and the bifrost-land pipeline.
		s.wirePipelineRecords(c)
		s.wireFullCI(c)
		rt.fullCI.Store(c.FullCI)
		wait := c.Start(ctx)
		<-ctx.Done()
		wait()
		pool.Close()
	})
	rt.stop = func() {
		cancel()
		<-done
	}
}

// agentRuntimeController is the platform-workers' controller. Every
// environment runs the same passes.
func agentRuntimeController(env string, db store.DB, src controller.Sources) *controller.Controller {
	return &controller.Controller{DB: db, Sources: src, Env: env, Logf: func(format string, args ...any) {
		slog.Warn("agentrt controller", "detail", fmt.Sprintf(format, args...))
	}, Infof: func(format string, args ...any) {
		slog.Info("agentrt controller", "detail", fmt.Sprintf(format, args...))
	}}
}

// pinReasonFromEnv is what an implement task pinned to the coordinator
// without a pin_reason gets (store/pinreason.go), read once at start from
// AGENTRT_PIN_REASON: warn (the default: accepted, counted as unexplained)
// or refuse (422). Any other value is logged and warn is used.
func pinReasonFromEnv() store.PinReasonMode {
	m, err := store.ParsePinReason(os.Getenv("AGENTRT_PIN_REASON"))
	if err != nil {
		slog.Error("agentrt pin reason", "error", err, "using", m)
	}
	return m
}

// agentrtRetry is how long connectAgentRuntime waits between attempts at
// first, and at most.
var agentrtRetry = [2]time.Duration{2 * time.Second, time.Minute}

// connectAgentRuntime opens until it can (or ctx ends: nil), each attempt
// with the start budget of review 156, waiting a little longer after each
// failure, and saying so in the log once a minute at most.
func connectAgentRuntime(ctx context.Context, open func(context.Context) (*pgxpool.Pool, error), retry [2]time.Duration) *pgxpool.Pool {
	wait, logged := retry[0], time.Time{}
	for attempt := 1; ; attempt++ {
		openCtx, openCancel := context.WithTimeout(ctx, 15*time.Second)
		pool, err := open(openCtx)
		openCancel()
		if err == nil {
			if attempt > 1 {
				slog.Info("agentrt: connected", "attempts", attempt)
			}
			return pool
		}
		if errors.Is(err, errMisconfigured) {
			// Retrying cannot mend the environment, the connection string or
			// the identity: the routes stay 503 until it is fixed and the
			// platform restarted (review 191 B2).
			slog.Error("agentrt: not connected and not retrying: the configuration is wrong; its routes answer 503", "err", err)
			return nil
		}
		if time.Since(logged) >= time.Minute {
			slog.Error("agentrt: not connected yet; its routes answer 503 and the platform keeps trying", "attempt", attempt, "err", err)
			logged = time.Now()
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
		wait = min(2*wait, retry[1])
	}
}

// lateDB is the runtime's database before and after it is connected.
type lateDB struct {
	pool atomic.Pointer[pgxpool.Pool]
	back atomic.Pointer[time.Time]
}

// open makes the routes ready on pool, at, the moment recorded first: no
// reader sees them ready without it (review 528 A1b).
func (l *lateDB) open(pool *pgxpool.Pool, at time.Time) {
	l.back.Store(&at)
	l.pool.Store(pool)
}

// Back is when the routes became ready; zero before.
func (l *lateDB) Back() time.Time {
	if t := l.back.Load(); t != nil {
		return *t
	}
	return time.Time{}
}

// errNotConnected is a use of the database before it is connected (the
// routes check Ready first).
var errNotConnected = errors.New("agentrt: the database is not connected yet")

// errMisconfigured is a failure no retry mends: an unknown environment, a
// connection string that does not parse, the wrong role or database.
var errMisconfigured = errors.New("agentrt: misconfigured")

func (l *lateDB) Ready() bool { return l.pool.Load() != nil }

func (l *lateDB) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	p := l.pool.Load()
	if p == nil {
		return nil, errNotConnected
	}
	return p.BeginTx(ctx, o)
}

// agentrtMaxConns bounds one process's pool. The environment's runtime role
// allows 10 connections on PROD and 5 on STG and DEV (infra db-step
// 2026-10-14-w31-agentrt-1-roles), shared by platform-api and
// platform-workers; pgxpool's default is one per CPU, 16 on the cluster's
// nodes, so the two processes sat at the limit at rest and a restarted one was
// refused ("too many connections for role") for up to a minute on every
// release (TD-392). Two processes at this size leave room under the limit.
func agentrtMaxConns(env string) int32 {
	if env == "prod" {
		return 4
	}
	return 2
}

// openAgentRuntime opens the pool and refuses an identity or a database that
// is not the environment's runtime ones.
func openAgentRuntime(ctx context.Context, dsn, env string) (*pgxpool.Pool, error) {
	if _, err := migrate.RuntimeRole(env); err != nil {
		return nil, fmt.Errorf("%w: %v", errMisconfigured, err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errMisconfigured, err)
	}
	if pw := os.Getenv("AGENTRT_DB_PASSWORD"); pw != "" {
		cfg.ConnConfig.Password = pw
	}
	cfg.MaxConns = agentrtMaxConns(env)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	var user, db string
	if err = pool.QueryRow(ctx, `SELECT current_user, current_database()`).Scan(&user, &db); err != nil {
		pool.Close()
		return nil, err
	}
	if err = identityFits(user, db, env); err != nil {
		pool.Close()
		return nil, fmt.Errorf("%w: %v", errMisconfigured, err)
	}
	return pool, nil
}

// identityFits accepts only the environment's runtime role on the
// environment's database.
func identityFits(user, db, env string) error {
	want, err := migrate.RuntimeRole(env)
	if err != nil {
		return err
	}
	if user != want || db != agentrtDatabase[env] {
		return fmt.Errorf("connected as %s to %s; want %s to %s", user, db, want, agentrtDatabase[env])
	}
	return nil
}

// agentRuntimeSources are the server's stores as the controller's sources.
// A nil store is passed as a nil reader, never as a typed nil.
func (s *Server) agentRuntimeSources() controller.Sources {
	var (
		ap  approvalReader
		ci  delivery.CIReader
		rel currentDeployment
	)
	if s.approvals != nil {
		ap = s.approvals
	}
	if s.agentRuntime != nil {
		ci = s.agentRuntime.evidenceCI
	}
	if s.releases != nil && s.releases.Service() != nil {
		rel = s.releases.Service()
	}
	src := agentRuntimeSources(ap, s.questionRuntime.agentRuntimeQuestion(), ci, rel)
	s.wireExternalRecords(&src)
	src.Ack = s.questionRuntime.agentRuntimeAck()
	src.Unacked = s.questionRuntime.agentRuntimeUnacked()
	src.Ask = s.questionRuntime.agentRuntimeAsk()
	src.Withdraw = s.questionRuntime.agentRuntimeWithdraw()
	src.Plan = s.agentRuntimePlanSources()
	return src
}

func (s *Server) mountAgentRuntime(r chi.Router) {
	if s.agentRuntime == nil {
		return
	}
	agentrtapi.Mount(r, s.auth, s.agentRuntime.api)
}

func (s *Server) closeAgentRuntime() {
	if s.agentRuntime != nil && s.agentRuntime.stop != nil {
		s.agentRuntime.stop()
	}
}
