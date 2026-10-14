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
	db   *agentrttest.DB
	rt   *pgx.Conn
	spec string
}

const (
	root = "R2"
	item = "R2:2.1"
	s1   = "R2:2.1-1"
	s2   = "R2:2.1-2"
	s3   = "R2:2.1-3"
)

func newWorld(t *testing.T) *world {
	t.Helper()
	db := agentrttest.Migrated(t)
	seeded, err := store.Seed(ctx, db.Connect(t, db.Owner))
	ok(t, err)
	if !seeded {
		t.Fatal("seed did nothing on an empty database")
	}
	w := &world{db: db, rt: db.Connect(t, db.Runtime)}
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
		return err
	}))
	return w
}

func (w *world) task(t *testing.T, mission, step, subject, role string) (string, error) {
	t.Helper()
	id := store.NewTaskID()
	err := store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		task := store.Task{ID: id, MissionID: mission, PlanNode: step, Subject: subject, Role: role,
			Class: "go", Title: "t", SpecArtifact: w.spec, Effect: "branch"}
		if step == "" {
			task.OffPlanReason = "outside the plan"
		}
		_, err := store.ProposeTask(ctx, tx, "controller", task)
		return err
	})
	return id, err
}

func (w *world) lastSeq(t *testing.T, stream string) int64 {
	t.Helper()
	var n int64
	ok(t, w.rt.QueryRow(ctx, `SELECT last_seq FROM agentrt.event_stream WHERE event_stream_id = $1`, stream).Scan(&n))
	return n
}

// T19: the seed needs no disabled trigger; mission, plan nodes, task and
// attempt are each first created in their own transaction; an event that
// names a row never created makes the commit fail and leaves no trace.
func TestSeedAndFirstCreation(t *testing.T) {
	w := newWorld(t)
	again, err := store.Seed(ctx, w.db.Connect(t, w.db.Owner))
	ok(t, err)
	if again {
		t.Fatal("seed ran twice")
	}
	t1, err := w.task(t, "M-1", s1, "", "implement")
	ok(t, err)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, _, err = store.LeaseAttempt(ctx, tx, "controller", "M-1", t1, "exec:codex", "", 1, 2*time.Minute)
		return err
	}))

	before := w.lastSeq(t, store.MissionStream("M-1"))
	err = store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err = store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-1"), Type: "task_proposed", Actor: "controller",
			MissionID: "M-1", TaskID: store.NewTaskID(), Body: map[string]any{"title": "never created", "role": "other"}})
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
	w := newWorld(t)
	t1, err := w.task(t, "M-1", s1, "", "implement")
	ok(t, err)
	var attempt string
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		attempt, _, err = store.LeaseAttempt(ctx, tx, "controller", "M-1", t1, "exec:codex", "", 1, 2*time.Minute)
		return err
	}))

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
	wantErr(t, exec(w.rt, `UPDATE agentrt.task SET state = 'ready' WHERE task_id = $1`, t1), "needs SET LOCAL agentrt.event_id")
	wantErr(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err = tx.Exec(ctx, `SELECT set_config('agentrt.event_id', '1', true)`)
		ok(t, err)
		_, err = tx.Exec(ctx, `UPDATE agentrt.task SET state = 'ready' WHERE task_id = $1`, t1)
		return err
	}), "was not written by this transaction")
	// An event of this transaction about another task.
	other, err := w.task(t, "M-1", s1, "", "implement")
	ok(t, err)
	wantErr(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-1"), Type: "task_state_changed", Actor: "controller",
			MissionID: "M-1", PlanNode: s1, TaskID: other, Body: map[string]any{"from": "proposed", "to": "ready"}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.task SET state = 'ready' WHERE task_id = $1`, t1)
		return err
	}), "task_id")
	// With its own event it goes through.
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-1"), Type: "task_state_changed", Actor: "controller",
			MissionID: "M-1", PlanNode: s1, TaskID: t1, Body: map[string]any{"from": "proposed", "to": "ready"}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.task SET state = 'ready', version = version + 1 WHERE task_id = $1`, t1)
		return err
	}))

	// Liveness columns: alone, without an event; the lease only forward;
	// together with another column, an event again.
	ok(t, exec(w.rt, `UPDATE agentrt.attempt SET heartbeat_at = clock_timestamp(), lease_expires_at = lease_expires_at + interval '30 seconds' WHERE attempt_id = $1`, attempt))
	wantErr(t, exec(w.rt, `UPDATE agentrt.attempt SET lease_expires_at = lease_expires_at - interval '1 second' WHERE attempt_id = $1`, attempt), "only moves forward")
	wantErr(t, exec(w.rt, `UPDATE agentrt.attempt SET heartbeat_at = clock_timestamp(), state = 'running' WHERE attempt_id = $1`, attempt), "needs SET LOCAL")

	// The reader reads and does nothing else; another environment's identity
	// does not get in.
	reader := w.db.Connect(t, w.db.Reader)
	var n int
	ok(t, reader.QueryRow(ctx, `SELECT count(*) FROM agentrt.event`).Scan(&n))
	wantErr(t, exec(reader, `INSERT INTO agentrt.control (control_id, scope, control_kind, reason, set_by_actor_id) VALUES ('c', 'global', 'stop', 'x', 'owner')`), "permission denied")
	wantErr(t, exec(reader, `UPDATE agentrt.task SET title = 'x'`), "permission denied")
	if _, err := pgx.Connect(ctx, w.db.Foreign); err == nil {
		t.Fatal("platform_agentrt_stg connected to a dev database")
	} else if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("foreign identity: %v", err)
	}
}

// T23b: a superuser edits an event body behind the triggers; the verifier
// names the stream and seq.
func TestTamperIsDetected(t *testing.T) {
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

// T32: links between the plan and the work are checked by the database.
func TestPlanLinks(t *testing.T) {
	w := newWorld(t)
	_, err := w.task(t, "M-1", item, "", "implement")
	wantErr(t, err, "must hang on a step")
	_, err = w.task(t, "M-2", s1, "", "implement")
	wantErr(t, err, "belongs to M-1")
	t1, err := w.task(t, "M-1", s1, "", "implement")
	ok(t, err)
	_, err = w.task(t, "M-1", s3, t1, "review")
	wantErr(t, err, "must sit where its subject")
	_, err = w.task(t, "M-1", s1, t1, "review")
	ok(t, err)
	off, err := w.task(t, "M-1", "", "", "other")
	ok(t, err)
	_ = off

	// Rebinding a step that has tasks, and moving a task, need a plan change;
	// a plan change that leaves a task of another mission on a step rolls back.
	// An event whose keys match but whose type is not plan_changed.
	wantErr(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err = store.Append(ctx, tx, store.Event{Stream: store.PlanStream(root), Type: "plan_node_retitled", Actor: "controller",
			PlanNode: s1, MissionID: "M-2", Body: map[string]any{"title": "s1"}}); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE agentrt.plan_node SET mission_id = 'M-2' WHERE plan_node_id = $1`, s1)
		return err
	}), "authorized plan change")
	// An event about this step but of the old mission: the key check stops it.
	wantErr(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err = store.SetStepState(ctx, tx, "controller", root, s1, "active", "test"); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE agentrt.plan_node SET mission_id = 'M-2' WHERE plan_node_id = $1`, s1)
		return err
	}), `has mission_id="M-1"`)
	wantErr(t, planChange(w, func(tx pgx.Tx) error {
		_, err = tx.Exec(ctx, `UPDATE agentrt.plan_node SET mission_id = 'M-2' WHERE plan_node_id = $1`, s1)
		return err
	}), "belongs to M-2 but task")

	// A done rule may name only tasks on its step and bound approvals.
	t2, err := w.task(t, "M-1", s3, "", "implement")
	ok(t, err)
	wantErr(t, planChange(w, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE agentrt.plan_node SET done_rule_json = jsonb_build_object('task_accepted', $2::text) WHERE plan_node_id = $1`, s1, t2)
		return err
	}), "not on this step")
	wantErr(t, planChange(w, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE agentrt.plan_node SET done_rule_json = '{"approval_executed":17}' WHERE plan_node_id = $1`, s1)
		return err
	}), "not bound to this step")
	ok(t, planChange(w, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE agentrt.plan_node SET done_rule_json = jsonb_build_object('task_accepted', $2::text) WHERE plan_node_id = $1`, s1, t1)
		return err
	}))
	// Moving t1 off the step its rule names rolls back too.
	wantErr(t, planChange(w, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE agentrt.task SET plan_node_id = $2 WHERE task_id = $1`, t1, s3)
		return err
	}), "")

	// Dependencies: a cycle is refused.
	a, _ := w.task(t, "M-1", s3, "", "implement")
	b, _ := w.task(t, "M-1", s3, "", "implement")
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { _, err := store.AddDependency(ctx, tx, "controller", "M-1", a, b); return err }))
	wantErr(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { _, err := store.AddDependency(ctx, tx, "controller", "M-1", b, a); return err }), "cycle")
}

// T32: two transactions inserting A->B and B->A at the same time; the graph
// lock makes the second see the first. A and B are in different missions,
// so their events go to different streams and only the graph lock can make
// the second transaction wait.
func TestConcurrentDependencyCycle(t *testing.T) {
	w := newWorld(t)
	a, err := w.task(t, "M-1", s3, "", "implement")
	ok(t, err)
	b, err := w.task(t, "M-2", s2, "", "implement")
	ok(t, err)
	other := w.db.Connect(t, w.db.Runtime)

	tx1, err := w.rt.Begin(ctx)
	ok(t, err)
	defer func() { _ = tx1.Rollback(ctx) }()
	_, err = store.AddDependency(ctx, tx1, "controller", "M-1", a, b)
	ok(t, err)
	done := make(chan error, 1)
	go func() {
		done <- store.InTx(ctx, other, func(tx pgx.Tx) error {
			_, err := store.AddDependency(ctx, tx, "controller", "M-2", b, a)
			return err
		})
	}()
	select {
	case err := <-done:
		t.Fatalf("second transaction did not wait for the graph lock: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	ok(t, tx1.Commit(ctx))
	wantErr(t, <-done, "cycle")
}

// T32 (review 143): moving a step to another mission while a task of the old
// mission is being attached to it, in both orders.
//
// Two shapes: the step already carries t1, which moves with it (the task
// update locks the step FOR UPDATE); and an empty unbound step that is only
// bound (no task moves, so only the step lock taken when a task is attached
// makes the two wait for each other).
func TestStepLockSerializesMoveAndAttach(t *testing.T) {
	type tc struct {
		name      string
		step      string
		moveTask  bool
		moveFirst bool
	}
	for _, c := range []tc{
		{"move_first", s1, true, true}, {"attach_first", s1, true, false},
		{"bind_first", s2, false, true}, {"attach_then_bind", s2, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			var t1 string
			if c.moveTask {
				var err error
				t1, err = w.task(t, "M-1", c.step, "", "implement")
				ok(t, err)
			}
			other := w.db.Connect(t, w.db.Runtime)
			moveFirst, step := c.moveFirst, c.step

			move := func(tx pgx.Tx) error {
				if _, err := store.Append(ctx, tx, store.Event{Stream: store.PlanStream(root), Type: "plan_changed", Actor: "controller",
					MissionID: "M-2", PlanNode: step, Body: map[string]any{"change": "migrate"}}); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `UPDATE agentrt.plan_node SET mission_id = 'M-2' WHERE plan_node_id = $1`, step); err != nil {
					return err
				}
				if t1 == "" {
					return nil
				}
				_, err := tx.Exec(ctx, `UPDATE agentrt.task SET mission_id = 'M-2' WHERE task_id = $1`, t1)
				return err
			}
			attach := func(tx pgx.Tx) error {
				_, err := store.ProposeTask(ctx, tx, "controller", store.Task{MissionID: "M-1", PlanNode: step, Role: "implement",
					Class: "go", Title: "t2", SpecArtifact: w.spec, Effect: "branch"})
				return err
			}
			first, second := move, attach
			if !moveFirst {
				first, second = attach, move
			}
			tx1, err := w.rt.Begin(ctx)
			ok(t, err)
			defer func() { _ = tx1.Rollback(ctx) }()
			ok(t, first(tx1))
			done := make(chan error, 1)
			go func() { done <- store.InTx(ctx, other, second) }()
			select {
			case err := <-done:
				t.Fatalf("second transaction did not wait for the step lock: %v", err)
			case <-time.After(300 * time.Millisecond):
			}
			ok(t, tx1.Commit(ctx))
			wantErr(t, <-done, "")

			var bad int
			ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.task t JOIN agentrt.plan_node s ON s.plan_node_id = t.plan_node_id
			  WHERE s.mission_id IS NOT NULL AND s.mission_id <> t.mission_id`).Scan(&bad))
			if bad != 0 {
				t.Fatalf("%d tasks hang on a step of another mission", bad)
			}
		})
	}
}

// T32: the counting rule over steps read back from the database.
func TestCountingFromTheDatabase(t *testing.T) {
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
