package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
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
// The controller reads no outside record yet (approvals, questions, CI runs,
// deployments): every rule atom that needs one stays unknown, so nothing is
// judged done on facts it cannot see. Pushed branches cannot be checked until a
// Gitea reader is configured; persist and salvage of a branch answer 503.
func (s *Server) wireAgentRuntime(role config.Role) {
	rt := &agentRuntime{api: &agentrtapi.API{Logf: func(format string, args ...any) {
		slog.Warn("agentrt", "detail", fmt.Sprintf(format, args...))
	}}}
	s.agentRuntime = rt
	dsn := strings.TrimSpace(os.Getenv("AGENTRT_DSN"))
	if dsn == "" {
		slog.Info("agentrt: AGENTRT_DSN is not set; its routes answer 503")
		return
	}
	// A database that does not answer delays the platform's start by at most
	// this much (review 156).
	openCtx, openCancel := context.WithTimeout(context.Background(), 15*time.Second)
	pool, err := openAgentRuntime(openCtx, dsn, strings.TrimSpace(os.Getenv("AGENTRT_ENV")))
	openCancel()
	if err != nil {
		slog.Error("agentrt: not connected; its routes answer 503", "err", err)
		return
	}
	rt.api.DB = pool
	rt.stop = pool.Close
	if !role.RunsWorkers() {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &controller.Controller{DB: pool, Sources: unreadableSources(), Logf: func(format string, args ...any) {
		slog.Warn("agentrt controller", "detail", fmt.Sprintf(format, args...))
	}}
	wait := c.Start(ctx)
	rt.stop = func() {
		cancel()
		wait()
		pool.Close()
	}
}

// openAgentRuntime opens the pool and refuses an identity or a database that
// is not the environment's runtime ones.
func openAgentRuntime(ctx context.Context, dsn, env string) (*pgxpool.Pool, error) {
	if _, err := migrate.RuntimeRole(env); err != nil {
		return nil, err
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
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
		return nil, err
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

// unreadableSources read nothing yet: approvals, questions, CI runs and
// deployments are wired in a later step.
func unreadableSources() controller.Sources {
	return controller.Sources{
		Approval: func(context.Context, int64) (string, bool) { return "", false },
		Question: func(context.Context, string) (store.Question, bool) { return store.Question{}, false },
		CI:       func(context.Context, string, string) (string, string, bool) { return "", "", false },
		Deployed: func(context.Context, string) (string, map[string]any, bool) { return "", nil, false },
	}
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
