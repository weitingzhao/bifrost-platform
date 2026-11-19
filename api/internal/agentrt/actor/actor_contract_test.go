package actor_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Origin: 2026-11-16, the review of the foundation code found the base's
// core packages tested from the inside only, and agentrt.actor is a table
// the base declares as shared storage (config/domain-ownership.json,
// contracts.shared_storage). This is the actor package's contract for the
// packages that ask it instead of the table, from outside the package, on a
// real database.

func seeded(t *testing.T) eventlog.DB {
	t.Helper()
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	if _, err := actor.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	return db.Pool(t, db.Runtime)
}

// What a name says an actor is.
func TestKindOfReadsTheName(t *testing.T) {
	for name, want := range map[string]string{
		"owner": "owner", "controller": "controller", "thread:local_1": "thread", "seat:coordinator": "seat",
		"host:agent-06": "host", "exec:codex": "executor", "agent:outside": "agent",
		"service:gate-git": "", "nobody": "", "": "",
	} {
		if got := actor.KindOf(name); got != want {
			t.Errorf("KindOf(%q) = %q, want %q", name, got, want)
		}
	}
}

// A controller-kind actor is registered once: the row, and one
// actor_registered event by the controller that names it; asking again
// writes nothing. Exists and ActorKind answer for it afterwards, and not
// before.
func TestRegisterControllerRegistersOnce(t *testing.T) {
	ctx := context.Background()
	pool := seeded(t)
	const id = "service:contract-test"
	events := func(tx pgx.Tx) int {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'actor_registered'
		  AND body_json->'actors' ? $1`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	err := eventlog.InTx(ctx, pool, func(tx pgx.Tx) error {
		if have, err := actor.Exists(ctx, tx, id); err != nil || have {
			t.Fatalf("before: Exists = %v, %v", have, err)
		}
		if _, err := actor.ActorKind(ctx, tx, id); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("before: ActorKind: %v", err)
		}
		// The positive control: the seeded controller is there.
		if have, err := actor.Exists(ctx, tx, "controller"); err != nil || !have {
			t.Fatalf("the seeded controller: Exists = %v, %v", have, err)
		}
		for i := 0; i < 2; i++ {
			if err := actor.RegisterController(ctx, tx, id, "Contract test", "registered by the actor package's contract test"); err != nil {
				return err
			}
			if n := events(tx); n != 1 {
				t.Fatalf("after call %d: %d actor_registered events name it, want 1", i+1, n)
			}
		}
		if have, err := actor.Exists(ctx, tx, id); err != nil || !have {
			t.Fatalf("after: Exists = %v, %v", have, err)
		}
		if kind, err := actor.ActorKind(ctx, tx, id); err != nil || kind != "controller" {
			t.Fatalf("after: ActorKind = %q, %v", kind, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A writer is checked against the table: a name that is no actor name and an
// actor that is not registered are refused as invalid; a registered one of
// the kind its name says passes.
func TestCheckWriterHoldsANameToTheTable(t *testing.T) {
	ctx := context.Background()
	pool := seeded(t)
	err := eventlog.InTx(ctx, pool, func(tx pgx.Tx) error {
		if registered, err := actor.CheckWriter(ctx, tx, actor.Writer{Actor: "controller"}); err != nil || !registered {
			t.Fatalf("the seeded controller: %v, %v", registered, err)
		}
		if _, err := actor.CheckWriter(ctx, tx, actor.Writer{Actor: "nobody"}); !errors.Is(err, eventlog.ErrInvalid) {
			t.Fatalf("a name that is no actor name: %v", err)
		}
		if registered, err := actor.CheckWriter(ctx, tx, actor.Writer{Actor: "seat:not-registered"}); !errors.Is(err, eventlog.ErrInvalid) || registered {
			t.Fatalf("a seat that is not registered: %v, %v", registered, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Expired keys go and live ones stay; a key that has not expired cannot be
// deleted at all (the table's guard), so the purge never picks one.
//
// Origin: 2026-11-19, 14,182 of PROD's 18,152 idempotency keys had expired
// and nothing deleted them (OWNER_LAW 42).
func TestPurgeExpiredKeysLeavesLiveOnes(t *testing.T) {
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	owner := db.Connect(t, db.Owner)
	if _, err := owner.Exec(ctx, `INSERT INTO agentrt.idempotency_key (idempotency_key_id, route, request_sha256, response_json, created_at, expires_at)
	  SELECT 'old-' || i, 'r', repeat('a', 64), '{}'::jsonb, clock_timestamp() - interval '8 days', clock_timestamp() - interval '1 hour' FROM generate_series(1, 5) i
	  UNION ALL SELECT 'live', 'r', repeat('a', 64), '{}'::jsonb, clock_timestamp(), clock_timestamp() + interval '7 days'`); err != nil {
		t.Fatal(err)
	}
	n, err := actor.PurgeExpiredKeys(ctx, db.Pool(t, db.Runtime), 2, 10)
	if err != nil || n != 5 {
		t.Fatalf("purged %d, %v; want the 5 expired", n, err)
	}
	var left []string
	rows, err := owner.Query(ctx, `SELECT idempotency_key_id FROM agentrt.idempotency_key ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	if left, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil || len(left) != 1 || left[0] != "live" {
		t.Fatalf("left %v, %v; want only the live key", left, err)
	}
}
