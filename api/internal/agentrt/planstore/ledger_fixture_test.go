package planstore_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planimport"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/storekit"
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

func wantErr(t *testing.T, err error, contains string) {
	t.Helper()
	if err == nil {
		t.Fatalf("succeeded; want an error containing %q", contains)
	}
	if !strings.Contains(err.Error(), contains) {
		t.Fatalf("err = %v; want it to contain %q", err, contains)
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

// world is a migrated, seeded database with two missions and a small plan:
// round R2 > item R2:2.1 > steps S1 (bound to M-1), S2 (unbound), S3 (bound
// to M-1).
type world struct {
	db *agentrttest.DB
	rt *pgx.Conn
	// t is the test the world was made for and ctx its context (ctxOf), for
	// a helper that has the world and no t.
	t    *testing.T
	ctx  context.Context
	spec string
	// impl is an implementation's spec as a plan's is (store.ImplementSpec there),
	// the one a proposed implementation needs (R5:5.7-8).
	impl string
}

const (
	root = "R2"
	item = "R2:2.1"
	s1   = "R2:2.1-1"
	s2   = "R2:2.1-2"
	s3   = "R2:2.1-3"
)

// The missions, plan and artifacts below stay per test: a mission's deadline
// and creation time, and the events' times, are read against the test's own
// clock, which many tests freeze before they make their world.
func newWorld(t *testing.T) *world {
	t.Helper()
	ctx := ctxOf(t)
	db := seededDB(t)
	w := &world{db: db, rt: db.Connect(t, db.Runtime), t: t, ctx: ctx}
	var err error
	deadline := time.Now().Add(7 * 24 * time.Hour)
	for _, m := range []string{"M-1", "M-2"} {
		ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			_, err = storekit.CreateMission(ctx, tx, "controller", storekit.Mission{ID: m, WorkID: "W-31", Title: m, Goal: "test", Deadline: deadline})
			return err
		}))
	}
	nodes := []planstore.PlanNode{
		{ID: root, Kind: "round", Label: "2", Title: "round 2", Root: root},
		{ID: item, Parent: root, Kind: "item", Label: "2.1", Title: "data layer", Root: root},
		{ID: s1, Parent: item, Kind: "step", Label: "2.1-1", Title: "s1", MissionID: "M-1", StepKind: "agent", Ordinal: 0, Root: root},
		{ID: s2, Parent: item, Kind: "step", Label: "2.1-2", Title: "s2", StepKind: "agent", Ordinal: 1, Root: root},
		{ID: s3, Parent: item, Kind: "step", Label: "2.1-3", Title: "s3", MissionID: "M-1", StepKind: "review", Ordinal: 2, Root: root},
	}
	for _, n := range nodes {
		ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			_, err = planstore.CreatePlanNode(ctx, tx, "controller", n)
			return err
		}))
	}
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		w.spec, err = artifact.PutArtifact(ctx, tx, "controller", "task_spec", "text/markdown", []byte("# task\n"))
		if err != nil {
			return err
		}
		w.impl, err = artifact.PutArtifact(ctx, tx, "controller", "task_spec", "application/json", []byte(`{"task":"task"}`))
		return err
	}))
	return w
}

// newKernelWorld is a world with mission M-1 active. In store it also makes
// hosts, jobs and candidates for the retired dispatch lane; no plan test reads
// them.
func newKernelWorld(t *testing.T) *world {
	t.Helper()
	ctx := ctxOf(t)
	w := newWorld(t)
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error { return storekit.MoveMission(ctx, tx, "controller", "M-1", "active", "test") }))
	return w
}

// waitFor returns once a backend of this database waits for a lock in a
// statement containing fragment, and fails the test if that has not happened
// by the deadline: a race the test means to stage did not happen.
func (w *world) waitFor(t *testing.T, fragment string, deadline time.Time) {
	t.Helper()
	w.waitForN(t, fragment, 1, deadline)
}

// waitForN is waitFor for at least n backends.
func (w *world) waitForN(t *testing.T, fragment string, want int, deadline time.Time) {
	t.Helper()
	ctx := ctxOf(t)
	for {
		var n int
		ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database()
		  AND wait_event_type = 'Lock' AND position($1 in query) > 0`, fragment).Scan(&n))
		if n >= want {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("nothing waited for a lock in %q in time; the case proves nothing", fragment)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (w *world) chainHolds(t *testing.T, stream string) {
	t.Helper()
	if _, err := eventlog.VerifyStream(ctx, w.rt, stream); err != nil {
		t.Fatalf("%s: %v", stream, err)
	}
}

func is(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("err = %v; want %v", err, target)
	}
}

func same(t *testing.T, got, want map[string]any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("answer %v; want the first answer %v", got, want)
	}
}

func (w *world) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	ctx := ctxOf(t)
	var n int
	ok(t, w.rt.QueryRow(ctx, q, args...).Scan(&n))
	return n
}

// footprint is what a write could leave: actors, events, streams and their
// heads, outbox rows, idempotency keys, missions, tasks, dependencies,
// controls, artifacts and their bodies.
func (w *world) footprint(t *testing.T) string {
	t.Helper()
	ctx := ctxOf(t)
	var s string
	ok(t, w.rt.QueryRow(ctx, `SELECT concat_ws(' ',
	  (SELECT count(*) FROM agentrt.actor), (SELECT count(*) FROM agentrt.event), (SELECT count(*) FROM agentrt.event_stream),
	  (SELECT string_agg(event_stream_id || '=' || last_seq || ':' || last_sha256, ',' ORDER BY event_stream_id) FROM agentrt.event_stream),
	  (SELECT count(*) FROM agentrt.idempotency_key), (SELECT count(*) FROM agentrt.mission),
	  (SELECT count(*) FROM agentrt.control),
	  (SELECT count(*) FROM agentrt.artifact), (SELECT count(*) FROM agentrt.artifact_blob))`).Scan(&s))
	return s
}
