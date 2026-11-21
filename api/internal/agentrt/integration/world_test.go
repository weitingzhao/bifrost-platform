package integration_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/ownercard"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/storekit"
)

var ctx = context.Background()

func wantErr(t *testing.T, err error, contains string) {
	t.Helper()
	if err == nil {
		t.Fatalf("succeeded; want an error containing %q", contains)
	}
	if !strings.Contains(err.Error(), contains) {
		t.Fatalf("err = %v; want it to contain %q", err, contains)
	}
}

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

func (w *world) lastSeq(t *testing.T, stream string) int64 {
	t.Helper()
	ctx := ctxOf(t)
	var n int64
	ok(t, w.rt.QueryRow(ctx, `SELECT last_seq FROM agentrt.event_stream WHERE event_stream_id = $1`, stream).Scan(&n))
	return n
}

// T19: the seed needs no disabled trigger; mission, plan nodes, task and
// attempt are each first created in their own transaction; an event that
// names a row never created makes the commit fail and leaves no trace.
func TestSeedAndFirstCreation(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	again, err := actor.Seed(ctx, w.db.Connect(t, w.db.Owner))
	ok(t, err)
	if again {
		t.Fatal("seed ran twice")
	}
	// The row an event names must exist: a mission never created (a task's
	// id is no longer checked, its table is gone since 0020).
	before := w.lastSeq(t, "mission:M-1")
	err = eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err = eventlog.Append(ctx, tx, eventlog.Event{Stream: "mission:M-1", Type: "mission_state_changed", Actor: "controller",
			MissionID: "M-99", Body: map[string]any{"from": "proposed", "to": "active"}})
		return err
	})
	wantErr(t, err, "violates foreign key")
	if after := w.lastSeq(t, "mission:M-1"); after != before {
		t.Fatalf("seq moved from %d to %d after a rolled-back event", before, after)
	}
	for _, s := range []string{"system", planstore.PlanStream(root), "mission:M-1", "mission:M-2"} {
		if _, err := eventlog.VerifyStream(ctx, w.rt, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

// T19 and T25 (database half): what each identity can and cannot do.
func TestIdentitiesAndTheEventRule(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	var err error
	// The state row of the examples below is a mission and the liveness row
	// a host: the tasks' and attempts' tables are gone since 0020.
	w.seeded(t, func(tx pgx.Tx) { seedHost(t, tx, "h1") })

	exec := func(c *pgx.Conn, q string, args ...any) error { _, err = c.Exec(ctx, q, args...); return err }

	// Append-only, for the runtime role and the owner alike (T23a).
	owner := w.db.Connect(t, w.db.Owner)
	for _, c := range []*pgx.Conn{w.rt, owner} {
		wantErr(t, exec(c, `UPDATE agentrt.event SET body_json = '{}' WHERE event_id = 1`), "")
		wantErr(t, exec(c, `DELETE FROM agentrt.event WHERE event_id = 1`), "")
	}
	wantErr(t, exec(owner, `UPDATE agentrt.event SET body_json = '{}' WHERE event_id = 1`), "append only")
	wantErr(t, exec(owner, `TRUNCATE agentrt.event CASCADE`), "append only")
	wantErr(t, exec(w.rt, `CREATE TABLE agentrt.x (a int)`), "permission denied")
	wantErr(t, exec(w.rt, `DROP TABLE agentrt.event`), "must be owner")

	// A state row without an event of this transaction.
	wantErr(t, exec(w.rt, `UPDATE agentrt.mission SET state = 'active' WHERE mission_id = 'M-2'`), "needs SET LOCAL agentrt.event_id")
	wantErr(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err = tx.Exec(ctx, `SELECT set_config('agentrt.event_id', '1', true)`)
		ok(t, err)
		_, err = tx.Exec(ctx, `UPDATE agentrt.mission SET state = 'active' WHERE mission_id = 'M-2'`)
		return err
	}), "was not written by this transaction")
	// An event of this transaction about another mission.
	wantErr(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "mission:M-1", Type: "mission_state_changed", Actor: "controller",
			MissionID: "M-1", Body: map[string]any{"from": "proposed", "to": "active"}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.mission SET state = 'active' WHERE mission_id = 'M-2'`)
		return err
	}), "mission_id")
	// With its own event it goes through.
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "mission:M-2", Type: "mission_state_changed", Actor: "controller",
			MissionID: "M-2", Body: map[string]any{"from": "proposed", "to": "active"}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.mission SET state = 'active', version = version + 1 WHERE mission_id = 'M-2'`)
		return err
	}))

	// Liveness columns: alone, without an event; together with another
	// column, an event again.
	ok(t, exec(w.rt, `UPDATE agentrt.host SET last_poll_at = clock_timestamp() WHERE host_id = 'h1'`))
	wantErr(t, exec(w.rt, `UPDATE agentrt.host SET last_poll_at = clock_timestamp(), state = 'paused' WHERE host_id = 'h1'`), "needs SET LOCAL")

	// The reader reads and does nothing else; another environment's identity
	// does not get in.
	reader := w.db.Connect(t, w.db.Reader)
	var n int
	ok(t, reader.QueryRow(ctx, `SELECT count(*) FROM agentrt.event`).Scan(&n))
	wantErr(t, exec(reader, `INSERT INTO agentrt.control (control_id, scope, control_kind, reason, set_by_actor_id) VALUES ('c', 'global', 'stop', 'x', 'owner')`), "permission denied")
	wantErr(t, exec(reader, `UPDATE agentrt.mission SET title = 'x'`), "permission denied")
	if _, err := pgx.Connect(ctx, w.db.Foreign); err == nil {
		t.Fatal("platform_agentrt_stg connected to a dev database")
	} else if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("foreign identity: %v", err)
	}
}

// T23b: a superuser edits an event body behind the triggers; the verifier
// names the stream and seq.
func TestTamperIsDetected(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	admin := w.db.Connect(t, w.db.Admin)
	for _, q := range []string{
		`ALTER TABLE agentrt.event DISABLE TRIGGER event_append_only`,
		`UPDATE agentrt.event SET body_json = '{"title":"edited"}' WHERE event_stream_id = 'mission:M-2' AND seq = 1`,
		`ALTER TABLE agentrt.event ENABLE TRIGGER event_append_only`,
	} {
		_, err := admin.Exec(ctx, q)
		ok(t, err)
	}
	_, err := eventlog.VerifyStream(ctx, w.rt, "mission:M-2")
	var br *eventlog.ChainBreak
	if !errors.As(err, &br) || br.Seq != 1 {
		t.Fatalf("verify = %v, want a break at seq 1", err)
	}
	if _, err := eventlog.VerifyStream(ctx, w.rt, "mission:M-1"); err != nil {
		t.Fatalf("untouched stream: %v", err)
	}
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

// T30: numbering never changes, nodes are never deleted.
func TestPlanNumberingIsFixed(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	for q, want := range map[string]string{
		`UPDATE agentrt.plan_node SET ordinal = 9 WHERE plan_node_id = 'R2:2.1-1'`:                                "keeps its id",
		`UPDATE agentrt.plan_node SET parent_plan_node_id = 'R2' WHERE plan_node_id = 'R2:2.1-1'`:                 "keeps its id",
		`UPDATE agentrt.plan_node SET created_at = created_at - interval '1 day' WHERE plan_node_id = 'R2:2.1-1'`: "keeps its id",
		`UPDATE agentrt.plan_node SET plan_node_id = 'R2:2.1-9' WHERE plan_node_id = 'R2:2.1-1'`:                  "never changes",
	} {
		wantErr(t, planChange(w, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, q); return err }), want)
	}
	// The owner meets the deletion guard, not just a missing privilege.
	_, err := w.db.Connect(t, w.db.Owner).Exec(ctx, `DELETE FROM agentrt.plan_node WHERE plan_node_id = 'R2:2.1-2'`)
	wantErr(t, err, "not allowed")
	wantErr(t, planChange(w, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM agentrt.plan_node WHERE plan_node_id = 'R2:2.1-2'`)
		return err
	}), "")
	// A step under a round, an item under an item.
	wantErr(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := planstore.CreatePlanNode(ctx, tx, "controller", planstore.PlanNode{ID: "R2:x", Parent: root, Kind: "step", Label: "x", Title: "x", StepKind: "agent", Ordinal: 5, Root: root})
		return err
	}), "cannot sit under")
	// State only with its own event type.
	wantErr(t, planChange(w, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE agentrt.plan_node SET state = 'done' WHERE plan_node_id = $1`, s2)
		return err
	}), "plan_node_state_changed")
}

// T32: the counting rule over steps read back from the database.
func TestCountingFromTheDatabase(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := planstore.SetStepState(ctx, tx, "controller", root, s1, "done", "test"); err != nil {
			return err
		}
		_, err := planstore.SetStepState(ctx, tx, "controller", root, s2, "dropped", "test")
		return err
	}))
	rows, err := w.rt.Query(ctx, `SELECT plan_node_id, state FROM agentrt.plan_node WHERE parent_plan_node_id = $1 ORDER BY ordinal`, item)
	ok(t, err)
	var steps []plan.Step
	for rows.Next() {
		var s plan.Step
		ok(t, rows.Scan(&s.ID, &s.State))
		steps = append(steps, s)
	}
	ok(t, rows.Err())
	got := plan.CountSteps(steps)
	if got.Done != 1 || got.Total != 2 || got.State != plan.StateActive {
		t.Fatalf("count = %+v", got)
	}
}

const (
	commitA = "2333e00753f91c74c8b2f4d693a803f8e89e7892"
	commitB = "0b9e1f3c7a1d2e4f5a6b7c8d9e0f1a2b3c4d5e6f"
	noSuch  = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
)

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

func (w *world) artifact(t *testing.T, kind, body string) string {
	t.Helper()
	ctx := ctxOf(t)
	var id string
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		var err error
		id, err = artifact.PutArtifact(ctx, tx, "controller", kind, "text/plain", []byte(body))
		return err
	}))
	return id
}

func (w *world) control(t *testing.T, scope, ref, kind string) string {
	t.Helper()
	got, err := coord.RequestControl(ctx, w.rt, wr("owner"), "k-"+eventlog.NewULID(time.Now()), scope, ref, kind, "test")
	ok(t, err)
	return got["control_id"].(string)
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

var owner = actor.Writer{Actor: "owner", Token: "admin"}

// rows counts what an import writes: nodes, events, artifacts and their
// bodies, streams, idempotency keys (the outbox is retired, 0028).
func rows(t *testing.T, pool *pgxpool.Pool) [6]int {
	t.Helper()
	var c [6]int
	ok(t, pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM agentrt.plan_node), (SELECT count(*) FROM agentrt.event),
	  (SELECT count(*) FROM agentrt.artifact), (SELECT count(*) FROM agentrt.artifact_blob),
	  (SELECT count(*) FROM agentrt.event_stream), (SELECT count(*) FROM agentrt.idempotency_key)`).Scan(&c[0], &c[1], &c[2], &c[3], &c[4], &c[5]))
	return c
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
	card                     ownercard.RuntimeCard
	status, option, by, text string
	read                     bool
}

func (s *cardStore) put(c ownercard.RuntimeCard) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cards == nil {
		s.cards, s.acked = map[string]*storedCard{}, map[string]int{}
	}
	s.cards[c.ID] = &storedCard{card: c, status: "open"}
	s.order = append(s.order, c.ID)
}

// ask makes a card asked now; created moves it back.
func (s *cardStore) ask(_ context.Context, c ownercard.Card) (string, error) {
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
		s.put(ownercard.RuntimeCard{ID: id, TaskID: c.TaskID, Recommended: rec, CreatedAt: time.Now(), Options: keys})
	}
	return id, nil
}

// asker is a fake question store: it records the cards and answers them
// later, as the Owner would.
type asker struct {
	mu      sync.Mutex
	cards   []ownercard.Card
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

func (a *asker) ask(_ context.Context, c ownercard.Card) (string, error) {
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

// What the plan tests still in this package use of the helpers of those that
// moved to planstore (TD-582); a small copy each, gone with the last of them.

// answered is a full answer: option, who, when.
func answered(id, option string) seenfacts.Question {
	return seenfacts.Question{ID: id, Status: seenfacts.QuestionAnswered, Option: option, Text: "yes", AnsweredBy: "owner",
		AnsweredAt: "2026-10-14T12:00:00Z"}
}

// Readers of outside records that always return the given value.
func approvalIs(status string) func(context.Context) (seenfacts.ApprovalFact, bool) {
	return func(context.Context) (seenfacts.ApprovalFact, bool) {
		return seenfacts.ApprovalFact{Status: status, Action: "test", Tier: "test"}, true
	}
}

func questionIs(q seenfacts.Question) func(context.Context) (seenfacts.Question, bool) {
	return func(context.Context) (seenfacts.Question, bool) { return q, true }
}

// deployments is a fake delivery record: what each environment runs, or
// that it cannot be read.
type deployment struct {
	commit string
	ref    map[string]any
}

type deployments struct {
	mu sync.Mutex
	m  map[string]deployment
}

func (d *deployments) read(_ context.Context, env string) (string, map[string]any, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	x, ok := d.m[env]
	return x.commit, x.ref, ok
}

func (w *world) evaluate(t *testing.T, step string, d *deployments) string {
	t.Helper()
	ctx := ctxOf(t)
	moved, err := planstore.EvaluateStep(ctx, w.rt, step, d.read)
	ok(t, err)
	return moved
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

func (w *world) stepState(t *testing.T, id string) string {
	t.Helper()
	ctx := ctxOf(t)
	var s string
	ok(t, w.rt.QueryRow(ctx, `SELECT state FROM agentrt.plan_node WHERE plan_node_id = $1`, id).Scan(&s))
	return s
}

// arch makes the ARCH root and its rows in order, each [id, rule] with its
// rule set by a plan change when there is one.
func (w *world) arch(t *testing.T, rows ...[2]string) {
	t.Helper()
	ctx := ctxOf(t)
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := planstore.CreatePlanNode(ctx, tx, "controller", planstore.PlanNode{ID: "ARCH", Kind: "arch_root", Label: "ARCH", Title: "架构对照",
			Root: "ARCH"}); err != nil {
			return err
		}
		for i, r := range rows {
			label := strings.TrimPrefix(r[0], "ARCH:")
			if _, err := planstore.CreatePlanNode(ctx, tx, "controller", planstore.PlanNode{ID: r[0], Parent: "ARCH", Kind: "arch_row", Label: label,
				Title: label, Ordinal: i, Root: "ARCH"}); err != nil {
				return err
			}
		}
		return nil
	}))
	for _, r := range rows {
		if r[1] == "" {
			continue
		}
		ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: planstore.PlanStream("ARCH"), Type: "plan_changed", Actor: "controller",
				Body: map[string]any{"change": "set_done_rule"}}); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `UPDATE agentrt.plan_node SET done_rule_json = $2::jsonb WHERE plan_node_id = $1`, r[0], r[1])
			return err
		}))
	}
}

// seenCards is a question store that keeps each card as the runtime asked
// it, so a test can read it as the Owner sees it.
type seenCards struct {
	cardStore
	asked map[string]ownercard.Card
}

func (s *seenCards) ask(ctx context.Context, c ownercard.Card) (string, error) {
	id, err := s.cardStore.ask(ctx, c)
	if err == nil {
		s.mu.Lock()
		if s.asked == nil {
			s.asked = map[string]ownercard.Card{}
		}
		s.asked[id] = c
		s.mu.Unlock()
	}
	return id, err
}

// ruleText is a rule's text with its header.
func ruleText(id string, version int, direction, effective, expires, body string) string {
	return fmt.Sprintf("rule_id: %s\nrule_version: %d\ndirection: %s\neffective_at: %s\nexpires_at: %s\n%s",
		id, version, direction, effective, expires, body)
}

const (
	since = "2026-10-01T00:00:00Z"
	until = "2027-01-15T00:00:00Z"
)

// A test's own event clock. A test that moves time gives itself one
// (ownClock) and calls the store with the context that carries it; it
// freezes that clock (freezeClock), not the process's (FreezeEventClock), so
// it runs while its neighbours run, each at its own time. The helpers a test
// calls ask for its context by the test (ctxOf): the test's own when it has
// a clock, a plain one otherwise, so one helper serves both kinds of test.

// testClock reads the wall clock until it is frozen.
type testClock struct{ at atomic.Pointer[time.Time] }

func (c *testClock) now() time.Time {
	if at := c.at.Load(); at != nil {
		return *at
	}
	return time.Now()
}

type ownTime struct {
	clock *testClock
	ctx   context.Context
}

// ownTimes holds each running test's clock under the test's name: a key per
// test, written when the test starts and removed when it ends, so tests
// running at once do not meet in it.
var ownTimes sync.Map

// ownTimeOf is the clock of t: its own, or the one of the test it is a
// subtest of (nearest first); nil when neither has one.
func ownTimeOf(t testing.TB) *ownTime {
	for name := t.Name(); ; {
		if o, ok := ownTimes.Load(name); ok {
			return o.(*ownTime)
		}
		i := strings.LastIndex(name, "/")
		if i < 0 {
			return nil
		}
		name = name[:i]
	}
}

// ownClock gives t an event clock of its own, the wall clock until t freezes
// it, and returns the context that carries it. A test calls it first (after
// t.Parallel()) and names the result ctx, which hides the package's ctx from
// the rest of its body; its subtests and the helpers it calls with its t
// find the same context (ctxOf).
func ownClock(t testing.TB) context.Context {
	t.Helper()
	c := &testClock{}
	o := &ownTime{clock: c, ctx: eventlog.WithEventClock(context.Background(), c.now)}
	name := t.Name()
	if _, dup := ownTimes.LoadOrStore(name, o); dup {
		t.Fatalf("%s already has an event clock of its own", name)
	}
	// Registered first, so it runs after every cleanup the test adds: those
	// still read the test's clock.
	t.Cleanup(func() { ownTimes.Delete(name) })
	return o.ctx
}

// ctxOf is the context a helper calls the store with on behalf of t: the one
// carrying t's own event clock when it has one, a plain one otherwise.
func ctxOf(t testing.TB) context.Context {
	if o := ownTimeOf(t); o != nil {
		return o.ctx
	}
	return context.Background()
}

// freezeClock makes t's own event clock read at, as FreezeEventClock does
// for the process's; the function it returns gives the wall clock back. A
// test with no clock of its own fails here: its store calls would not read
// the frozen time.
func freezeClock(t testing.TB, at time.Time) func() {
	t.Helper()
	o := ownTimeOf(t)
	if o == nil {
		t.Fatalf("%s has no event clock of its own: call ownClock(t) first", t.Name())
		return nil
	}
	o.clock.at.Store(&at)
	return func() { o.clock.at.Store(nil) }
}

// SetHook installs a test hook (see hook) and returns the function that
// removes it.
func SetHook(f func(point, arg string)) func() {
	actor.TestHook.Store(&f)
	return func() { actor.TestHook.Store(nil) }
}

// SeenCopy is an approval_seen or question_seen event as the external facts
// test reads it back: Ref is the approval number or question id.
type SeenCopy struct {
	Ref, Status, Option, AnsweredBy, AnsweredAt, TaskID string
	At                                                  time.Time
	EventID                                             int64
}

// SeenCopiesForTest reads the approval_seen or question_seen copies of refs,
// in event order.
func SeenCopiesForTest(ctx context.Context, tx pgx.Tx, typ, key string, refs []string) ([]SeenCopy, error) {
	var tasks []string
	rows, err := tx.Query(ctx, `SELECT event_id,at,COALESCE(task_id,''),COALESCE(body_json->>$2,''),COALESCE(body_json->>'status',''),COALESCE(body_json->>'option',''),COALESCE(body_json->>'answered_by',''),COALESCE(body_json->>'answered_at','') FROM agentrt.event WHERE event_type=$1 AND (body_json->>$2=ANY($3) OR task_id=ANY($4)) ORDER BY event_id`, typ, key, refs, tasks)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (SeenCopy, error) {
		var c SeenCopy
		return c, r.Scan(&c.EventID, &c.At, &c.TaskID, &c.Ref, &c.Status, &c.Option, &c.AnsweredBy, &c.AnsweredAt)
	})
}
