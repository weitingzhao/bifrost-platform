package runtimedb

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
)

// Each half of the identity is checked on its own, and an environment that
// does not exist is refused (review 156 B2); no database needed.
func TestAgentRuntimeIdentity(t *testing.T) {
	for _, c := range []struct {
		user, db, env string
		ok            bool
	}{
		{"platform_agentrt_dev", "bifrost_platform_dev", "dev", true},
		{"platform_agentrt_prod", "bifrost_platform", "prod", true},
		{"platform_agentrt_stg", "bifrost_platform_stg", "stg", true},
		{"platform_agentrt_stg", "bifrost_platform_dev", "dev", false},
		{"platform_agentrt_dev", "bifrost_platform", "dev", false},
		{"postgres", "bifrost_platform_dev", "dev", false},
		{"platform_agentrt_qa", "bifrost_platform_qa", "qa", false},
		{"platform_agentrt_", "", "", false},
	} {
		if err := identityFits(c.user, c.db, c.env); (err == nil) != c.ok {
			t.Errorf("%s on %s for %q: %v", c.user, c.db, c.env, err)
		}
	}
}

// The pool is refused unless it is the environment's runtime role on the
// environment's database.
func TestAgentRuntimeRefusesAnotherIdentity(t *testing.T) {
	db := agentrttest.Migrated(t)
	for _, dsn := range []string{db.Admin, db.Runtime} {
		if pool, err := open(context.Background(), dsn, "dev"); err == nil {
			pool.Close()
			t.Fatalf("accepted %q", dsn)
		}
	}
	if _, err := open(context.Background(), db.Runtime, "qa"); err == nil {
		t.Fatal("accepted an environment that does not exist")
	}
}

// A refusal at start (a pod the network policy does not know yet) heals by
// itself: the runtime keeps opening until it can, and stops when told to.
func TestAgentRuntimeKeepsConnecting(t *testing.T) {
	db := agentrttest.Migrated(t)
	attempts := 0
	pool := connect(context.Background(), func(ctx context.Context) (*pgxpool.Pool, error) {
		attempts++
		if attempts < 3 {
			return nil, errors.New("connect: connection refused")
		}
		return pgxpool.New(ctx, db.Runtime)
	}, [2]time.Duration{time.Millisecond, 5 * time.Millisecond})
	if pool == nil || attempts != 3 {
		t.Fatalf("pool %v after %d attempts; want one on the third", pool, attempts)
	}
	pool.Close()
}

// No database needed: cancelling ends the attempts with no pool, and a
// configuration no retry mends is tried once (review 191 B1, B2).
func TestAgentRuntimeStopsConnecting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	if p := connect(ctx, func(context.Context) (*pgxpool.Pool, error) {
		return nil, errors.New("connect: connection refused")
	}, [2]time.Duration{time.Millisecond, 5 * time.Millisecond}); p != nil {
		t.Fatal("a pool after the context ended")
	}
	attempts := 0
	if p := connect(context.Background(), func(context.Context) (*pgxpool.Pool, error) {
		attempts++
		return nil, fmt.Errorf("%w: the role is not the environment's", errMisconfigured)
	}, [2]time.Duration{time.Millisecond, 5 * time.Millisecond}); p != nil || attempts != 1 {
		t.Fatalf("a misconfiguration: pool %v after %d attempts; want none after one", p, attempts)
	}
	if _, err := open(context.Background(), "host=x", "qa"); !errors.Is(err, errMisconfigured) {
		t.Fatalf("an unknown environment: %v; want misconfigured", err)
	}
}

// Until the database is connected the routes answer 503 not_connected; once
// it is, they work.
func TestAgentRuntimeRoutesWaitForTheConnection(t *testing.T) {
	db := agentrttest.Migrated(t)
	late := &Late{}
	if late.Ready() {
		t.Fatal("ready before connecting")
	}
	if _, err := late.BeginTx(context.Background(), pgx.TxOptions{}); !errors.Is(err, errNotConnected) {
		t.Fatalf("BeginTx before connecting: %v", err)
	}
	pool, err := pgxpool.New(context.Background(), db.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	late.pool.Store(pool)
	tx, err := late.BeginTx(context.Background(), pgx.TxOptions{})
	if err != nil || !late.Ready() {
		t.Fatalf("after connecting: %v", err)
	}
	_ = tx.Rollback(context.Background())
}

// TD-392: two processes (platform-api and platform-workers) share the
// runtime role's connection limit (10 on PROD, 5 on STG and DEV); their pools
// together stay under it, with room for a restarted process.
func TestAgentRuntimePoolsFitTheRoleLimit(t *testing.T) {
	for env, limit := range map[string]int32{"prod": 10, "stg": 5, "dev": 5} {
		if n := maxConns(env); n < 2 || 2*n >= limit {
			t.Errorf("%s: two pools of %d against a limit of %d", env, n, limit)
		}
	}
}
