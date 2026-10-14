package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// agentrtDatabase is the state database of each environment (design v5
// §5.1.1).
var agentrtDatabase = map[string]string{
	"prod": "bifrost_platform",
	"stg":  "bifrost_platform_stg",
	"dev":  "bifrost_platform_dev",
}

// runAgentrtMigrate is `platform-api agentrt-migrate`: it applies the agentrt
// migrations and the seed, as agentrt_owner_<env>, to that environment's
// database. The connection comes from the libpq environment (PGHOST, PGPORT,
// PGDATABASE, PGUSER, PGPASSWORD, PGSSLMODE); AGENTRT_ENV names the
// environment. It refuses a database or user that is not the environment's.
func runAgentrtMigrate() int {
	env := os.Getenv("AGENTRT_ENV")
	want, ok := agentrtDatabase[env]
	if !ok {
		log.Printf("agentrt-migrate: AGENTRT_ENV must be dev, stg or prod, got %q", env)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	conn, err := pgx.Connect(ctx, "")
	if err != nil {
		log.Printf("agentrt-migrate: connect: %v", err)
		return 1
	}
	defer func() { _ = conn.Close(context.Background()) }()
	applied, seeded, err := migrateAndSeed(ctx, conn, env, want)
	if err != nil {
		log.Printf("agentrt-migrate: %v", err)
		return 1
	}
	fmt.Printf("agentrt-migrate: env=%s database=%s applied=%v seeded=%v\n", env, want, applied, seeded)
	return 0
}

func migrateAndSeed(ctx context.Context, conn *pgx.Conn, env, wantDB string) ([]int, bool, error) {
	var db, user string
	if err := conn.QueryRow(ctx, `SELECT current_database(), current_user`).Scan(&db, &user); err != nil {
		return nil, false, err
	}
	if db != wantDB || user != "agentrt_owner_"+env {
		return nil, false, fmt.Errorf("connected to %s as %s; %s needs %s as agentrt_owner_%s", db, user, env, wantDB, env)
	}
	applied, err := migrate.Run(ctx, conn, env)
	if err != nil {
		return applied, false, err
	}
	seeded, err := store.Seed(ctx, conn)
	return applied, seeded, err
}
