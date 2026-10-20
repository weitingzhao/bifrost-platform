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
	"strconv"
	"strings"
	"sync"
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
	return newFrom(t, "")
}

// newFrom is New, the database a copy of template when one is named.
func newFrom(t testing.TB, template string) *DB {
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
	create := fmt.Sprintf(`CREATE DATABASE %s OWNER agentrt_owner_dev`, name)
	if template != "" {
		create += " TEMPLATE " + template
	}
	for _, q := range []string{
		create,
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

// Migrated is New followed by the migrations, run as the owner: a copy of a
// database this test process migrated once (a package's tests reached CI's
// 120 s running the migrations in every one, 2026-10-18).
func Migrated(t testing.TB) *DB {
	t.Helper()
	b := base(t)
	tmplOnce.Do(func() { tmplName, tmplErr = makeTemplate(b) })
	if tmplErr != nil {
		t.Fatalf("migrated template: %v", tmplErr)
	}
	return newFrom(t, tmplName)
}

var (
	tmplOnce sync.Once
	tmplName string
	tmplErr  error
)

// templatePrefix names the migrated databases tests copy; one older than
// templateStale was left by a test process that ended without dropping it.
const (
	templatePrefix = "agentrt_tmpl_"
	templateStale  = 6 * time.Hour
)

// makeTemplate creates and migrates the database this process copies. It is
// left when the process ends (a package's tests have no end hook here) and
// dropped by a later one once stale.
func makeTemplate(b string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, b+" dbname=postgres")
	if err != nil {
		return "", err
	}
	defer func() { _ = admin.Close(ctx) }()
	rows, err := admin.Query(ctx, `SELECT datname FROM pg_database WHERE datname LIKE 'agentrt\_tmpl\_%'`)
	if err != nil {
		return "", err
	}
	old, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return "", err
	}
	for _, d := range old {
		stamp, _, _ := strings.Cut(strings.TrimPrefix(d, templatePrefix), "_")
		if at, perr := strconv.ParseInt(stamp, 10, 64); perr == nil && time.Since(time.Unix(at, 0)) > templateStale {
			_, _ = admin.Exec(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %s WITH (FORCE)`, d))
		}
	}
	var r [4]byte
	_, _ = rand.Read(r[:])
	name := fmt.Sprintf("%s%d_%s", templatePrefix, time.Now().Unix(), hex.EncodeToString(r[:]))
	for _, q := range []string{
		fmt.Sprintf(`CREATE DATABASE %s OWNER agentrt_owner_dev`, name),
		fmt.Sprintf(`REVOKE CONNECT ON DATABASE %s FROM PUBLIC`, name),
		fmt.Sprintf(`GRANT CONNECT ON DATABASE %s TO agentrt_owner_dev`, name),
	} {
		if _, err = admin.Exec(ctx, q); err != nil {
			return "", fmt.Errorf("%s: %w", q, err)
		}
	}
	owner, err := pgx.Connect(ctx, fmt.Sprintf("%s dbname=%s user=agentrt_owner_dev", b, name))
	if err != nil {
		return "", err
	}
	_, err = migrate.Run(ctx, owner, "dev")
	// No connection may stay on a template while it is copied.
	_ = owner.Close(ctx)
	if err != nil {
		return "", fmt.Errorf("migrate: %w", err)
	}
	return name, nil
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

// TestJobRule is the job rule the test fixtures' candidates belong to
// (job:test): routing counts a candidate only while its rule is in force and
// reads.
const TestJobRule = ruleInsert + `('job:test', 1,
    '{"candidates":[{"executor":"exec:codex","model_pattern":"*","provider":"openai","status":"active"}],"direction":"initial","effective_at":"2026-01-01T00:00:00Z","expires_at":"2100-01-01T00:00:00Z","rule_id":"job:test","rule_version":1,"tenant":"dev","text":"Test fixture candidates."}',
    E'rule_id: job:test\nrule_version: 1\ndirection: initial\neffective_at: 2026-01-01T00:00:00Z\nexpires_at: 2100-01-01T00:00:00Z\ntenant: dev\ntext: Test fixture candidates.\ncandidates:\n  - {executor: exec:codex, provider: openai, model_pattern: "*", status: active}\n',
    'initial', 'test', 'test', '2026-01-01T00:00:00Z', '2100-01-01T00:00:00Z') ON CONFLICT DO NOTHING`

// OwnerFactsRule is the capability-facts rule with the seed's four Owner
// facts, as if he had signed it; BindOwnerFacts binds the seed rows to it (run
// it with an event of the transaction, the import event in tests). Until a
// capability-facts rule binds them, an only fact grants nobody.
const OwnerFactsRule = ruleInsert + `('capability-facts', 1,
    '{"direction":"initial","effective_at":"2026-01-01T00:00:00Z","expires_at":"2100-01-01T00:00:00Z","facts":[{"executor":"exec:claude","kind":"only","scope":{"core":true},"subject":"author_core"},{"executor":"exec:codex","kind":"only","scope":{"core":true,"sandbox":"read-only"},"subject":"review_core"},{"executor":"exec:cursor","kind":"must_not","subject":"fix_review_findings"},{"executor":"exec:deterministic","kind":"must_not","subject":"review_own_provider"}],"rule_id":"capability-facts","rule_version":1}',
    E'rule_id: capability-facts\nrule_version: 1\ndirection: initial\neffective_at: 2026-01-01T00:00:00Z\nexpires_at: 2100-01-01T00:00:00Z\nfacts:\n  - {executor: exec:cursor, kind: must_not, subject: fix_review_findings}\n  - {executor: exec:claude, kind: only, subject: author_core, scope: {core: true}}\n  - {executor: exec:codex, kind: only, subject: review_core, scope: {core: true, sandbox: read-only}}\n  - {executor: exec:deterministic, kind: must_not, subject: review_own_provider}\n',
    'initial', 'test', 'test', '2026-01-01T00:00:00Z', '2100-01-01T00:00:00Z') ON CONFLICT DO NOTHING`

// BindOwnerFacts binds the seed's Owner facts to OwnerFactsRule.
const BindOwnerFacts = `UPDATE agentrt.capability_fact SET rule_id = 'capability-facts', rule_version = 1
  WHERE source_kind = 'owner' AND rule_id IS NULL`

// CardAutoRule is a card-auto-answer rule (TD-390) that answers blocked
// cards 30 minutes after they open, as if the Owner had signed it on STG.
const CardAutoRule = ruleInsert + `('card-auto-answer', 1,
    '{"after_minutes":30,"direction":"initial","effective_at":"2026-01-01T00:00:00Z","expires_at":"2100-01-01T00:00:00Z","kinds":["blocked"],"rule_id":"card-auto-answer","rule_version":1}',
    E'rule_id: card-auto-answer\nrule_version: 1\ndirection: initial\neffective_at: 2026-01-01T00:00:00Z\nexpires_at: 2100-01-01T00:00:00Z\nafter_minutes: 30\nkinds: [blocked]\n',
    'initial', 'test', 'test', '2026-01-01T00:00:00Z', '2100-01-01T00:00:00Z') ON CONFLICT DO NOTHING`
