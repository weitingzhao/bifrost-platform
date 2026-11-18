package planstore_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planimport"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
)

// The ledger fixture of the plan tests that moved here from package store
// (TD-582): a migrated database with the seed's actors, the writer the routes
// make, and a question store the tests answer by hand. It is the fixture those
// tests had in store, without store: nothing here imports it. The ports and
// the base's event vocabulary are the ones this test binary has because
// planobservations_ledger_test.go links store (contract_fixture_test.go says
// the same); the fixture binds nothing, since a binding is process-wide and
// these tests run in parallel.
//
// Tests that need a process-wide switch (actor.TestHook, a lowered
// PendingChangeLimit, the process's event clock) stay in store, where
// TestTestsRunInParallel watches them.

var ctx = context.Background()

func ok(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// wr is a writer as the routes make one: the actor and the token.
func wr(id string) actor.Writer { return actor.Writer{Actor: id, Token: "operator"} }

// seededTemplate names the template seedTemplate prepares.
const seededTemplate = "seeded"

// seedActors are the actors store.Seed writes (store/seed.go).
var seedActors = []struct{ id, kind, tool, name string }{
	{"controller", "controller", "", "Runtime controller"},
	{"owner", "owner", "", "Owner"},
	{"exec:claude", "executor", "claude", "Claude Code"},
	{"exec:codex", "executor", "codex", "Codex CLI"},
	{"exec:cursor", "executor", "cursor", "Cursor CLI"},
	{"exec:deterministic", "executor", "deterministic", "Deterministic checks"},
	{"seat:chief_of_staff", "seat", "", "Chief of staff seat"},
}

// seedTemplate writes what store.Seed writes on an empty database: the first
// event and the actors that hang on it, in one transaction.
func seedTemplate(ctx context.Context, owner *pgx.Conn) error {
	ids := make([]any, 0, len(seedActors))
	for _, a := range seedActors {
		ids = append(ids, a.id)
	}
	return eventlog.InTx(ctx, owner, func(tx pgx.Tx) error {
		if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "system", Type: "actor_registered", Actor: "controller",
			Body: map[string]any{"actors": ids, "note": "seed"}}); err != nil {
			return err
		}
		for _, a := range seedActors {
			if _, err := tx.Exec(ctx, `INSERT INTO agentrt.actor (actor_id, actor_kind, tool, display_name) VALUES ($1,$2,$3,$4)`,
				a.id, a.kind, eventlog.Nullable(a.tool), a.name); err != nil {
				return fmt.Errorf("seed actor %s: %w", a.id, err)
			}
		}
		return nil
	})
}

// seededDB is a migrated database the seed has run on: a copy of a template
// seeded once per test process.
func seededDB(t *testing.T) *agentrttest.DB {
	t.Helper()
	return agentrttest.MigratedWith(t, seededTemplate, seedTemplate)
}

func importDB(t *testing.T) (*pgxpool.Pool, planimport.Request) {
	t.Helper()
	db := seededDB(t)
	text, err := os.ReadFile("../planimport/testdata/ledger.md")
	ok(t, err)
	req, err := planimport.ParseLedger(string(text), 2, planimport.LedgerOptions{File: "WORK.md"})
	ok(t, err)
	return db.Pool(t, db.Runtime), req
}

// asker is a fake question store: it records the cards and answers them
// later, as the Owner would.
type asker struct {
	mu      sync.Mutex
	cards   []planstore.Card
	answers map[string]seenfacts.Question
	acked   map[string]int
}

func (a *asker) ack(_ context.Context, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.acked == nil {
		a.acked = map[string]int{}
	}
	a.acked[id]++
	return nil
}

func (a *asker) ask(_ context.Context, c planstore.Card) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i, prev := range a.cards {
		if prev.Key == c.Key {
			return "q_" + string(rune('a'+i)), nil
		}
	}
	a.cards = append(a.cards, c)
	return "q_" + string(rune('a'+len(a.cards)-1)), nil
}

func (a *asker) lastID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return "q_" + string(rune('a'+len(a.cards)-1))
}

func (a *asker) answer(id, status, option string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.answers == nil {
		a.answers = map[string]seenfacts.Question{}
	}
	a.answers[id] = seenfacts.Question{ID: id, Status: status, Option: option, AnsweredBy: "owner", AnsweredAt: "2026-10-14T20:00:00Z"}
}

func (a *asker) question(_ context.Context, id string) (seenfacts.Question, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	q, ok := a.answers[id]
	if !ok {
		return seenfacts.Question{ID: id, Status: "open"}, true
	}
	return q, true
}
