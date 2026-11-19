package store_test

import (
	"context"
	"errors"
	"fmt"
	"os"
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
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/storekit"
)

// The fixtures the rule tests stand on. The cross-domain tests left for
// agentrt/integration on 2026-11-18 and keep their own copy (world_test.go), so
// that this package does not depend on that one.

var ctx = context.Background()

func ctxOf(t testing.TB) context.Context { return store.CtxOf(t) }

func ok(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
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
	// impl is an implementation's spec as a plan's is (store.ImplementSpec),
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

// seededDB is a migrated database the seed has run on: a copy of a template
// seeded once per test process (CI-14), not a seed per test.
func seededDB(t *testing.T) *agentrttest.DB {
	t.Helper()
	return agentrttest.MigratedWith(t, seededTemplate, seedTemplate)
}

// seededTemplate names the template seedTemplate prepares. TestMain makes it
// before any test runs, so the seed's events carry the wall clock whatever
// event clock the first test to want a world has frozen.
const seededTemplate = "seeded"

func seedTemplate(ctx context.Context, owner *pgx.Conn) error {
	seeded, err := actor.Seed(ctx, owner)
	if err == nil && !seeded {
		err = errors.New("seed did nothing on an empty database")
	}
	return err
}

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

func planChange(w *world, fn func(tx pgx.Tx) error) error {
	ctx := w.ctx
	return eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: planstore.PlanStream(root), Type: "plan_changed", Actor: "controller",
			Body: map[string]any{"change": "test"}}); err != nil {
			return err
		}
		return fn(tx)
	})
}

const noSuch = "sha256:0000000000000000000000000000000000000000000000000000000000000000"

// newKernelWorld is a world with mission M-1 active and hosts h1–h8.
func newKernelWorld(t *testing.T) *world {
	t.Helper()
	ctx := ctxOf(t)
	w := newWorld(t)
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error { return storekit.MoveMission(ctx, tx, "controller", "M-1", "active", "test") }))
	w.seeded(t, func(tx pgx.Tx) {
		for i := 1; i <= 8; i++ {
			seedHost(t, tx, fmt.Sprintf("h%d", i))
		}
	})
	_, err := w.rt.Exec(ctx, agentrttest.DispatchRules)
	ok(t, err)
	return w
}

// Scenes of the retired dispatch lane, written as its rows (store cut 3b-2,
// 2026-11-14). Nothing leases, runs or checks a task any more, but the plan's
// bindings, the artifact search and the progress read still read the rows a
// round left: the tasks, their attempts and the verifications. These
// fixtures write those rows as an import does (a legacy_imported event of
// the transaction), in the shape the lane left them.

// seedHost makes host id exist: an attempt names the host it ran on.
func seedHost(t *testing.T, tx pgx.Tx, id string) {
	t.Helper()
	_, err := tx.Exec(ctx, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name)
 VALUES ('host:' || $1, 'host', 'Agent host ' || $1) ON CONFLICT DO NOTHING`, id)
	ok(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO agentrt.host (host_id, actor_id, host_kind, state)
 VALUES ($1, 'host:' || $1, 'dev', 'active') ON CONFLICT DO NOTHING`, id)
	ok(t, err)
}

// seeded runs fn in a transaction that carries an import event, as a
// fixture's rows need.
func (w *world) seeded(t *testing.T, fn func(tx pgx.Tx)) {
	t.Helper()
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test"}}); err != nil {
			return err
		}
		fn(tx)
		return nil
	}))
}

// Fixtures of the plan import, for the tests that start from an imported
// plan; the import's results through the routes are in
// agentrtapi/planimport_test.go.

func importDB(t *testing.T) (*pgxpool.Pool, planimport.Request) {
	t.Helper()
	db := seededDB(t)
	text, err := os.ReadFile("../planimport/testdata/ledger.md")
	ok(t, err)
	req, err := planimport.ParseLedger(string(text), 2, planimport.LedgerOptions{File: "WORK.md"})
	ok(t, err)
	return db.Pool(t, db.Runtime), req
}

// cardStore is a question store as the card-auto-answer rule sees it: cards
// asked (with when, and the option they recommend), open until answered.
type cardStore struct {
	mu      sync.Mutex
	cards   map[string]*storedCard
	order   []string
	answers int
	acked   map[string]int
	during  func(id string) // runs as an answer is written, before it lands
	after   func(id string) // runs once an answer has landed, before the rule's caller hears so
	ackErr  error           // what the next acknowledgement answers
}

type storedCard struct {
	card                     planstore.RuntimeCard
	status, option, by, text string
	read                     bool
}

func (s *cardStore) put(c planstore.RuntimeCard) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cards == nil {
		s.cards, s.acked = map[string]*storedCard{}, map[string]int{}
	}
	s.cards[c.ID] = &storedCard{card: c, status: "open"}
	s.order = append(s.order, c.ID)
}

// ask makes a card asked now; created moves it back.
func (s *cardStore) ask(_ context.Context, c planstore.Card) (string, error) {
	id := "q_" + c.Key[:16]
	s.mu.Lock()
	_, have := s.cards[id]
	s.mu.Unlock()
	if !have {
		rec := ""
		if c.Recommended > 0 {
			rec = string(rune('a' + c.Recommended - 1))
		}
		// The question store keys the options a, b, … in order.
		var keys []string
		for i := range c.Options {
			keys = append(keys, string(rune('a'+i)))
		}
		s.put(planstore.RuntimeCard{ID: id, TaskID: c.TaskID, Recommended: rec, CreatedAt: time.Now(), Options: keys})
	}
	return id, nil
}

// asker is a fake question store: it records the cards and answers them
// later, as the Owner would.
type asker struct {
	mu      sync.Mutex
	cards   []planstore.Card
	answers map[string]seenfacts.Question
	acked   map[string]int
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

// rule sets a node's done rule and binds approvals or questions to it, as an
// authorized plan change does.
func (w *world) rule(t *testing.T, node, rule string, binds ...[2]string) {
	t.Helper()
	ctx := ctxOf(t)
	ok(t, planChange(w, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE agentrt.plan_node SET done_rule_json = $2::jsonb WHERE plan_node_id = $1`, node, rule); err != nil {
			return err
		}
		for _, b := range binds {
			if _, err := tx.Exec(ctx, `INSERT INTO agentrt.plan_node_binding (plan_node_id, bound_kind, bound_ref, event_id)
			  VALUES ($1, $2, $3, current_setting('agentrt.event_id')::bigint)`, node, b[0], b[1]); err != nil {
				return err
			}
		}
		return nil
	}))
}

// step adds a step of M-1 under the item, with a done rule when rule is set.
func (w *world) step(t *testing.T, id string, ordinal int, rule string, binds ...[2]string) {
	t.Helper()
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := planstore.CreatePlanNode(ctx, tx, "controller", planstore.PlanNode{ID: id, Parent: item, Kind: "step", Label: id, Title: id,
			MissionID: "M-1", StepKind: "agent", Ordinal: ordinal, Root: root})
		return err
	}))
	if rule != "" {
		w.rule(t, id, rule, binds...)
	}
}

// seenCards is a question store that keeps each card as the runtime asked
// it, so a test can read it as the Owner sees it.
type seenCards struct {
	cardStore
	asked map[string]planstore.Card
}

func (s *seenCards) ask(ctx context.Context, c planstore.Card) (string, error) {
	id, err := s.cardStore.ask(ctx, c)
	if err == nil {
		s.mu.Lock()
		if s.asked == nil {
			s.asked = map[string]planstore.Card{}
		}
		s.asked[id] = c
		s.mu.Unlock()
	}
	return id, err
}
