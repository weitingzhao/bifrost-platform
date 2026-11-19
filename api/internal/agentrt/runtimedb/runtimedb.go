// Package runtimedb connects the agent runtime's database: a pool, refused
// unless it is the runtime role of the environment on that environment's
// database, opened in the background so a refusal at start heals by itself.
package runtimedb

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
)

// database is the state database of each environment (design v5
// §5.1.1); the migrate subcommand keeps the same table.
var database = map[string]string{
	"prod": "bifrost_platform",
	"stg":  "bifrost_platform_stg",
	"dev":  "bifrost_platform_dev",
}

// retryWait is how long connect waits between attempts at
// first, and at most.
var retryWait = [2]time.Duration{2 * time.Second, time.Minute}

// connect opens until it can (or ctx ends: nil), each attempt
// with the start budget of review 156, waiting a little longer after each
// failure, and saying so in the log once a minute at most.
func connect(ctx context.Context, opener func(context.Context) (*pgxpool.Pool, error), retry [2]time.Duration) *pgxpool.Pool {
	wait, logged := retry[0], time.Time{}
	for attempt := 1; ; attempt++ {
		openCtx, openCancel := context.WithTimeout(ctx, 15*time.Second)
		pool, err := opener(openCtx)
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

// Late is the runtime's database before and after it is connected.
type Late struct {
	pool atomic.Pointer[pgxpool.Pool]
	back atomic.Pointer[time.Time]
}

// Open makes the routes ready on pool, at, the moment recorded first: no
// reader sees them ready without it (review 528 A1b).
func (l *Late) Open(pool *pgxpool.Pool, at time.Time) {
	l.back.Store(&at)
	l.pool.Store(pool)
}

// Back is when the routes became ready; zero before.
func (l *Late) Back() time.Time {
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

func (l *Late) Ready() bool { return l.pool.Load() != nil }

func (l *Late) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	p := l.pool.Load()
	if p == nil {
		return nil, errNotConnected
	}
	return p.BeginTx(ctx, o)
}

// maxConns bounds one process's pool. The environment's runtime role
// allows 10 connections on PROD and 5 on STG and DEV (infra db-step
// 2026-10-14-w31-agentrt-1-roles), shared by platform-api and
// platform-workers; pgxpool's default is one per CPU, 16 on the cluster's
// nodes, so the two processes sat at the limit at rest and a restarted one was
// refused ("too many connections for role") for up to a minute on every
// release (TD-392). Two processes at this size leave room under the limit.
func maxConns(env string) int32 {
	if env == "prod" {
		return 4
	}
	return 2
}

// open opens the pool and refuses an identity or a database that
// is not the environment's runtime ones.
func open(ctx context.Context, dsn, env string) (*pgxpool.Pool, error) {
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
	cfg.MaxConns = maxConns(env)
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
	if user != want || db != database[env] {
		return fmt.Errorf("connected as %s to %s; want %s to %s", user, db, want, database[env])
	}
	return nil
}

// Connect opens the environment's pool, trying until it can; nil when ctx ended
// or the configuration is one no retry mends.
func Connect(ctx context.Context, dsn, env string) *pgxpool.Pool {
	return connect(ctx, func(ctx context.Context) (*pgxpool.Pool, error) {
		return open(ctx, dsn, env)
	}, retryWait)
}
