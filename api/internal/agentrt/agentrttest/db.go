// Package agentrttest gives tests a fresh agentrt database on a real
// PostgreSQL. AGENTRT_TEST_PG is a libpq connection string for a superuser
// on a server where the roles of the dev environment exist
// (agentrt_owner_dev, platform_agentrt_dev, agent_reader, plus
// platform_agentrt_stg to prove an identity of another environment is
// refused). Without it the tests skip, unless AGENTRT_TEST_REQUIRED=1, which
// CI sets once its Postgres sidecar exists: then a missing server fails.
package agentrttest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
)

// DB is one throwaway database with a connection string per identity.
type DB struct {
	Name    string
	Admin   string // superuser, the test's tamper hand (T23b)
	Owner   string // agentrt_owner_dev: migrations
	Runtime string // platform_agentrt_dev: the platform at run time
	Reader  string // agent_reader
	Foreign string // platform_agentrt_stg: an identity of another environment
}

func base(t testing.TB) string {
	t.Helper()
	dsn := os.Getenv("AGENTRT_TEST_PG")
	if dsn == "" {
		if os.Getenv("AGENTRT_TEST_REQUIRED") == "1" {
			t.Fatal("AGENTRT_TEST_PG is not set but AGENTRT_TEST_REQUIRED=1")
		}
		t.Skip("set AGENTRT_TEST_PG to a superuser connection string to run the agentrt database tests")
	}
	return dsn
}

// New creates an empty database owned by agentrt_owner_dev, with CONNECT
// revoked from PUBLIC and granted to the dev identities only, as the db-step
// does. It does not migrate.
func New(t testing.TB) *DB {
	t.Helper()
	b := base(t)
	var r [6]byte
	_, _ = rand.Read(r[:])
	name := "agentrt_test_" + hex.EncodeToString(r[:])
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, b+" dbname=postgres")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close(ctx) }()
	for _, q := range []string{
		fmt.Sprintf(`CREATE DATABASE %s OWNER agentrt_owner_dev`, name),
		fmt.Sprintf(`REVOKE CONNECT ON DATABASE %s FROM PUBLIC`, name),
		fmt.Sprintf(`GRANT CONNECT ON DATABASE %s TO agentrt_owner_dev, platform_agentrt_dev, agent_reader`, name),
	} {
		if _, err := admin.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c, err := pgx.Connect(ctx, b+" dbname=postgres")
		if err != nil {
			t.Logf("drop %s: %v", name, err)
			return
		}
		defer func() { _ = c.Close(ctx) }()
		if _, err := c.Exec(ctx, fmt.Sprintf(`DROP DATABASE %s WITH (FORCE)`, name)); err != nil {
			t.Logf("drop %s: %v", name, err)
		}
	})
	return &DB{
		Name:    name,
		Admin:   fmt.Sprintf("%s dbname=%s", b, name),
		Owner:   fmt.Sprintf("%s dbname=%s user=agentrt_owner_dev", b, name),
		Runtime: fmt.Sprintf("%s dbname=%s user=platform_agentrt_dev", b, name),
		Reader:  fmt.Sprintf("%s dbname=%s user=agent_reader", b, name),
		Foreign: fmt.Sprintf("%s dbname=%s user=platform_agentrt_stg", b, name),
	}
}

// Migrated is New followed by the migrations, run as the owner.
func Migrated(t testing.TB) *DB {
	t.Helper()
	db := New(t)
	ctx := context.Background()
	c := db.Connect(t, db.Owner)
	if _, err := migrate.Run(ctx, c, "dev"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// Connect opens a connection closed when the test ends.
func (db *DB) Connect(t testing.TB, dsn string) *pgx.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

// Redis returns a client and a key prefix of its own, so tests share one
// server without seeing each other's keys. AGENTRT_TEST_REDIS is a
// redis:// or unix:// URL; the skip and require rules are those of the
// database. The prefix's keys are removed when the test ends.
func Redis(t testing.TB) (*redis.Client, string) {
	t.Helper()
	url := os.Getenv("AGENTRT_TEST_REDIS")
	if url == "" {
		if os.Getenv("AGENTRT_TEST_REQUIRED") == "1" {
			t.Fatal("AGENTRT_TEST_REDIS is not set but AGENTRT_TEST_REQUIRED=1")
		}
		t.Skip("set AGENTRT_TEST_REDIS to run the agentrt Redis tests")
	}
	opt, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	c := redis.NewClient(opt)
	var r [6]byte
	_, _ = rand.Read(r[:])
	prefix := "agrt-test-" + hex.EncodeToString(r[:]) + ":"
	t.Cleanup(func() {
		DropPrefix(t, c, prefix)
		_ = c.Close()
	})
	return c, prefix
}

// DropPrefix deletes every key under prefix, the test's FLUSH.
func DropPrefix(t testing.TB, c *redis.Client, prefix string) {
	t.Helper()
	ctx := context.Background()
	iter := c.Scan(ctx, 0, prefix+"*", 500).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		t.Fatal(err)
	}
	if len(keys) > 0 {
		if err := c.Del(ctx, keys...).Err(); err != nil {
			t.Fatal(err)
		}
	}
}

// Pool opens a pgx pool closed when the test ends.
func (db *DB) Pool(t testing.TB, dsn string) *pgxpool.Pool {
	t.Helper()
	p, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

// The two rules the Owner signs that dispatch needs, as rows a test inserts
// as the runtime role: concurrency (one live attempt a host) and the dev
// tenant's budget (missions that state none get 1000 attempts and 720 hours;
// an attempt at most a day). Without both in force the runtime dispatches
// nothing. Each row's text and body_json agree (store's TestFixtureRule reads
// them back).
const (
	ConcurrencyRule = ruleInsert + concurrencyRow + ` ON CONFLICT DO NOTHING`
	DevBudgetRule   = ruleInsert + devBudgetRow + ` ON CONFLICT DO NOTHING`
	DispatchRules   = ruleInsert + concurrencyRow + ",\n" + devBudgetRow + ` ON CONFLICT DO NOTHING`
)

const ruleInsert = `INSERT INTO agentrt.rule (rule_id, rule_version, body_json, body_text, direction, signed_by, signature,
    effective_at, expires_at) VALUES `

const concurrencyRow = `('concurrency', 1,
    '{"direction":"initial","effective_at":"2026-01-01T00:00:00Z","expires_at":"2100-01-01T00:00:00Z","per_host":1,"rule_id":"concurrency","rule_version":1}',
    E'rule_id: concurrency\nrule_version: 1\ndirection: initial\neffective_at: 2026-01-01T00:00:00Z\nexpires_at: 2100-01-01T00:00:00Z\nper_host: 1\n',
    'initial', 'test', 'test', '2026-01-01T00:00:00Z', '2100-01-01T00:00:00Z')`

const devBudgetRow = `('budget:dev', 1,
    '{"attempt_hard_limit_s":86400,"direction":"initial","effective_at":"2026-01-01T00:00:00Z","expires_at":"2100-01-01T00:00:00Z","mission":{"max_attempts":1000,"max_wall_hours":720},"rule_id":"budget:dev","rule_version":1,"tenant":"dev"}',
    E'rule_id: budget:dev\nrule_version: 1\ndirection: initial\neffective_at: 2026-01-01T00:00:00Z\nexpires_at: 2100-01-01T00:00:00Z\ntenant: dev\nmission: {max_attempts: 1000, max_wall_hours: 720}\nattempt_hard_limit_s: 86400\n',
    'initial', 'test', 'test', '2026-01-01T00:00:00Z', '2100-01-01T00:00:00Z')`
