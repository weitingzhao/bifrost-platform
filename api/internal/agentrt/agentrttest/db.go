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
		asyncCommit(name),
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

// asyncCommit lets every session of a throwaway database commit without
// waiting for its WAL to reach the disk. A test database never outlives a
// crash of its server, so nothing a test can observe changes: visibility,
// locks, isolation and constraint checks are those of synchronous commit.
// The setting belongs to the database, so every connection to it has it,
// whatever DSN opened it; CREATE DATABASE does not copy it from a template,
// so each database gets its own. On a server whose fsync really flushes the
// disk the store package took 794 s instead of 1003 s (R5:5.7-4,
// GO_TEST_TIMING.md); it changes nothing on the server itself.
func asyncCommit(name string) string {
	return fmt.Sprintf(`ALTER DATABASE %s SET synchronous_commit = off`, name)
}

// Migrated is New followed by the migrations, run as the owner: a copy of a
// database this test process migrated once (a package's tests reached CI's
// 120 s running the migrations in every one, 2026-10-18).
func Migrated(t testing.TB) *DB {
	t.Helper()
	return MigratedWith(t, "", nil)
}

// Prepare writes, as the owner, what every test of a package would otherwise
// write first into its own copy (store.Seed, say).
type Prepare func(ctx context.Context, owner *pgx.Conn) error

// MigratedWith is Migrated from a template this process migrated and then
// prepared once, one template per key (CI-14: the store package's 1,300
// tests each ran the seed in their own copy). What prepare writes must not
// depend on when a test runs: the template is made by the process's first
// test that asks for key, and every later one copies it as it was then.
// The empty key with a nil prepare is Migrated's template.
func MigratedWith(t testing.TB, key string, prepare Prepare) *DB {
	t.Helper()
	b := base(t)
	tm := template(key)
	tm.once.Do(func() { tm.name, tm.err = makeTemplate(b, prepare) })
	if tm.err != nil {
		t.Fatalf("migrated template %q: %v", key, tm.err)
	}
	return newFrom(t, tm.name)
}

// Warm makes key's template now, outside any test, when AGENTRT_TEST_PG
// names a server: a package whose prepare must not run under a test's state
// (an event clock a test froze, say) calls it from TestMain. Without a server
// it does nothing, and an error waits for the first MigratedWith of key.
func Warm(key string, prepare Prepare) {
	b := os.Getenv("AGENTRT_TEST_PG")
	if b == "" {
		return
	}
	tm := template(key)
	tm.once.Do(func() { tm.name, tm.err = makeTemplate(b, prepare) })
}

type tmpl struct {
	once sync.Once
	name string
	err  error
}

func template(key string) *tmpl {
	tmplMu.Lock()
	defer tmplMu.Unlock()
	tm := tmpls[key]
	if tm == nil {
		tm = &tmpl{}
		tmpls[key] = tm
	}
	return tm
}

var (
	tmplMu sync.Mutex
	tmpls  = map[string]*tmpl{}
)

// templatePrefix names the migrated databases tests copy; one older than
// templateStale was left by a test process that ended without dropping it.
const (
	templatePrefix = "agentrt_tmpl_"
	templateStale  = 6 * time.Hour
)

// makeTemplate creates, migrates and prepares (when prepare is not nil) a
// database this process copies. It is left when the process ends (a
// package's tests have no end hook here) and dropped by a later one once
// stale.
func makeTemplate(b string, prepare Prepare) (string, error) {
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
		asyncCommit(name),
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
	if err != nil {
		_ = owner.Close(ctx)
		return "", fmt.Errorf("migrate: %w", err)
	}
	if prepare != nil {
		err = prepare(ctx, owner)
	}
	// No connection may stay on a template while it is copied.
	_ = owner.Close(ctx)
	if err != nil {
		return "", fmt.Errorf("prepare: %w", err)
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
