package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/relay"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
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
}

// wireAgentRuntime connects the agent runtime (design v5 §5.2) when
// AGENTRT_DSN is set (a libpq connection string without the password; the
// password comes from AGENTRT_DB_PASSWORD, which the Deployment fills from
// the Secret agentrt-db-runtime): a pool, refused unless it is the runtime role of
// AGENTRT_ENV on that environment's database; the routes; and in the workers
// process the controller loops. Without it the routes answer 503 and nothing
// runs, so the code ships before the database is wired into the Deployment.
//
// The controller reads approvals, questions, CI runs and deployments from the
// platform's own stores (agentRuntimeSources); a record it cannot read leaves
// the rule atoms on it unknown, so nothing is judged done on facts it cannot
// see. Pushed branches are checked on Gitea in the organization
// AGENTRT_GIT_ORG (agentrt_heads.go); without it, persist and salvage of a
// branch answer 503.
//
// With AGENTRT_REDIS_ADDR (redis-agents of the environment; the password in
// AGENTRT_REDIS_PASSWORD, from the Secret redis-agents) the workers process
// also runs the relay: committed outbox rows go to redis-agents as signals,
// and a new or wiped Redis is rebuilt from PostgreSQL first (design v5
// §5.1.4, W-31 2.3). PostgreSQL stays the record; without Redis nothing is
// lost, readers only wait for their next poll.
func (s *Server) wireAgentRuntime(role config.Role) {
	rt := &agentRuntime{api: &agentrtapi.API{Logf: func(format string, args ...any) {
		slog.Warn("agentrt", "detail", fmt.Sprintf(format, args...))
	}, Ask: s.questionRuntime.agentRuntimeAsk(), Anchor: releasepolicy.OwnerKeyFingerprint, Head: s.agentRuntimeHeads(), MainHead: s.agentRuntimeMains()}}
	s.agentRuntime = rt
	dsn := strings.TrimSpace(os.Getenv("AGENTRT_DSN"))
	if dsn == "" {
		slog.Info("agentrt: AGENTRT_DSN is not set; its routes answer 503")
		return
	}
	// The routes answer 503 until the database is connected; the platform
	// keeps connecting in the background, so a refusal at start (a new pod
	// the network policy does not know yet, a database restarting) heals by
	// itself instead of leaving the runtime off until the next restart.
	late := &lateDB{}
	rt.api.DB = late
	rdb, prefix, rerr := agentrtRedisFromEnv()
	switch {
	case rerr != nil:
		slog.Error("agentrt: redis-agents is configured wrongly; no relay, PostgreSQL alone carries the runtime", "err", rerr)
	case rdb == nil:
		slog.Info("agentrt: AGENTRT_REDIS_ADDR is not set; no relay, PostgreSQL alone carries the runtime")
	}
	if rdb != nil {
		rt.api.Actions = &actions.Store{Redis: rdb, Prefix: prefix}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	env := strings.TrimSpace(os.Getenv("AGENTRT_ENV"))
	safego.Go("agentrt-connect", func() {
		defer close(done)
		pool := connectAgentRuntime(ctx, func(ctx context.Context) (*pgxpool.Pool, error) {
			return openAgentRuntime(ctx, dsn, env)
		}, agentrtRetry)
		if pool == nil {
			if rdb != nil {
				_ = rdb.Close()
			}
			return
		}
		late.pool.Store(pool)
		if !role.RunsWorkers() {
			<-ctx.Done()
			pool.Close()
			if rdb != nil {
				_ = rdb.Close()
			}
			return
		}
		c := &controller.Controller{DB: pool, Sources: s.agentRuntimeSources(), Briefs: true, Logf: func(format string, args ...any) {
			slog.Warn("agentrt controller", "detail", fmt.Sprintf(format, args...))
		}}
		wait := c.Start(ctx)
		relayDone := make(chan struct{})
		if rdb == nil {
			close(relayDone)
		} else {
			r := &relay.Relay{PG: pool, Redis: rdb, Prefix: prefix}
			safego.Go("agentrt-relay", func() {
				defer close(relayDone)
				slog.Info("agentrt relay: started", "prefix", prefix)
				_ = r.Run(ctx)
			})
		}
		<-ctx.Done()
		wait()
		<-relayDone
		pool.Close()
		if rdb != nil {
			_ = rdb.Close()
		}
	})
	rt.stop = func() {
		cancel()
		<-done
	}
}

// agentrtRedisPrefix is a key prefix of the runtime: "agrt:" in PROD and STG
// (each has its own redis-agents), "agrt-dev:" on DEV's shared Redis.
var agentrtRedisPrefix = regexp.MustCompile(`^agrt(-[a-z]+)?:$`)

// agentrtRedisFromEnv is the redis-agents client of AGENTRT_REDIS_ADDR, or nil
// when it is not set. The password is required (redis-agents refuses to start
// without one) and the prefix must be the runtime's, so the relay cannot write
// over another tenant's keys on a shared Redis.
func agentrtRedisFromEnv() (*redis.Client, string, error) {
	addr := strings.TrimSpace(os.Getenv("AGENTRT_REDIS_ADDR"))
	if addr == "" {
		return nil, "", nil
	}
	prefix := strings.TrimSpace(os.Getenv("AGENTRT_REDIS_PREFIX"))
	if prefix == "" {
		prefix = "agrt:"
	}
	if !agentrtRedisPrefix.MatchString(prefix) {
		return nil, "", fmt.Errorf("AGENTRT_REDIS_PREFIX %q is not a runtime prefix (agrt: or agrt-<env>:)", prefix)
	}
	pw := os.Getenv("AGENTRT_REDIS_PASSWORD")
	if pw == "" {
		return nil, "", errors.New("AGENTRT_REDIS_PASSWORD is empty")
	}
	// ContextTimeoutEnabled: a request's deadline bounds the dial, the reads
	// and the writes; without it go-redis keeps its own timeouts and a
	// heartbeat's 2-second budget for actions held 5.25 s against a silent
	// Redis (review 264 A2).
	return redis.NewClient(&redis.Options{Addr: addr, Password: pw, DialTimeout: 5 * time.Second, ContextTimeoutEnabled: true}), prefix, nil
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
type lateDB struct{ pool atomic.Pointer[pgxpool.Pool] }

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
		ci  ciIndexer
		rel currentDeployment
	)
	if s.approvals != nil {
		ap = s.approvals
	}
	if s.delivery != nil {
		ci = s.delivery.PolicyFacts()
	}
	if s.releases != nil && s.releases.Service() != nil {
		rel = s.releases.Service()
	}
	src := agentRuntimeSources(ap, s.questionRuntime.agentRuntimeQuestion(), ci, rel)
	src.Ack = s.questionRuntime.agentRuntimeAck()
	src.Ask = s.questionRuntime.agentRuntimeAsk()
	src.Withdraw = s.questionRuntime.agentRuntimeWithdraw()
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
