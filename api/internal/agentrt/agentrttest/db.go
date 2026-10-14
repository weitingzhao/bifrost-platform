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
