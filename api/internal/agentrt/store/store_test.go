package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

var ctx = context.Background()

// A test that moves time has an event clock of its own (ownclock_test.go):
// it starts with ctx := ownClock(t), which hides the ctx above, and freezes
// with freezeClock(t, at). A helper that calls the store starts with
// ctx := ctxOf(t): the test's own context when it has a clock, a plain one
// when it has none.
func ownClock(t testing.TB) context.Context {
	t.Helper()
	return store.OwnClock(t)
}

func ctxOf(t testing.TB) context.Context { return store.CtxOf(t) }

func freezeClock(t testing.TB, at time.Time) func() {
	t.Helper()
	return store.FreezeClock(t, at)
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
	seeded, err := store.Seed(ctx, owner)
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
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			_, err = store.CreateMission(ctx, tx, "controller", store.Mission{ID: m, WorkID: "W-31", Title: m, Goal: "test", Deadline: deadline})
			return err
		}))
	}
	nodes := []store.PlanNode{
		{ID: root, Kind: "round", Label: "2", Title: "round 2", Root: root},
		{ID: item, Parent: root, Kind: "item", Label: "2.1", Title: "data layer", Root: root},
		{ID: s1, Parent: item, Kind: "step", Label: "2.1-1", Title: "s1", MissionID: "M-1", StepKind: "agent", Ordinal: 0, Root: root},
		{ID: s2, Parent: item, Kind: "step", Label: "2.1-2", Title: "s2", StepKind: "agent", Ordinal: 1, Root: root},
		{ID: s3, Parent: item, Kind: "step", Label: "2.1-3", Title: "s3", MissionID: "M-1", StepKind: "review", Ordinal: 2, Root: root},
	}
	for _, n := range nodes {
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			_, err = store.CreatePlanNode(ctx, tx, "controller", n)
			return err
		}))
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		w.spec, err = store.PutArtifact(ctx, tx, "controller", "task_spec", "text/markdown", []byte("# task\n"))
		if err != nil {
			return err
		}
		w.impl, err = store.PutArtifact(ctx, tx, "controller", "task_spec", "application/json", []byte(`{"task":"task"}`))
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
	again, err := store.Seed(ctx, w.db.Connect(t, w.db.Owner))
	ok(t, err)
	if again {
		t.Fatal("seed ran twice")
	}
	// The row an event names must exist: a mission never created (a task's
	// id is no longer checked, its table is gone since 0020).
	before := w.lastSeq(t, store.MissionStream("M-1"))
	err = store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err = store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-1"), Type: "mission_state_changed", Actor: "controller",
			MissionID: "M-99", Body: map[string]any{"from": "proposed", "to": "active"}})
		return err
	})
	wantErr(t, err, "violates foreign key")
	if after := w.lastSeq(t, store.MissionStream("M-1")); after != before {
		t.Fatalf("seq moved from %d to %d after a rolled-back event", before, after)
	}
	for _, s := range []string{"system", store.PlanStream(root), store.MissionStream("M-1"), store.MissionStream("M-2")} {
		if _, err := store.VerifyStream(ctx, w.rt, s); err != nil {
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
	wantErr(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err = tx.Exec(ctx, `SELECT set_config('agentrt.event_id', '1', true)`)
		ok(t, err)
		_, err = tx.Exec(ctx, `UPDATE agentrt.mission SET state = 'active' WHERE mission_id = 'M-2'`)
		return err
	}), "was not written by this transaction")
	// An event of this transaction about another mission.
	wantErr(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-1"), Type: "mission_state_changed", Actor: "controller",
			MissionID: "M-1", Body: map[string]any{"from": "proposed", "to": "active"}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.mission SET state = 'active' WHERE mission_id = 'M-2'`)
		return err
	}), "mission_id")
	// With its own event it goes through.
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-2"), Type: "mission_state_changed", Actor: "controller",
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
	_, err := store.VerifyStream(ctx, w.rt, store.MissionStream("M-2"))
	var br *store.ChainBreak
	if !errors.As(err, &br) || br.Seq != 1 {
		t.Fatalf("verify = %v, want a break at seq 1", err)
	}
	if _, err := store.VerifyStream(ctx, w.rt, store.MissionStream("M-1")); err != nil {
		t.Fatalf("untouched stream: %v", err)
	}
}

func planChange(w *world, fn func(tx pgx.Tx) error) error {
	ctx := w.ctx
	return store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: store.PlanStream(root), Type: "plan_changed", Actor: "controller",
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
	wantErr(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: "R2:x", Parent: root, Kind: "step", Label: "x", Title: "x", StepKind: "agent", Ordinal: 5, Root: root})
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
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.SetStepState(ctx, tx, "controller", root, s1, "done", "test"); err != nil {
			return err
		}
		_, err := store.SetStepState(ctx, tx, "controller", root, s2, "dropped", "test")
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
