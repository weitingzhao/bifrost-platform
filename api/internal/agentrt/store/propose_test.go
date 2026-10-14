package store_test

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func (w *world) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	ok(t, w.rt.QueryRow(ctx, q, args...).Scan(&n))
	return n
}

// footprint is what a write could leave: actors, events, stream heads,
// idempotency keys, missions, tasks, dependencies, controls, artifacts.
func (w *world) footprint(t *testing.T) string {
	t.Helper()
	var s string
	ok(t, w.rt.QueryRow(ctx, `SELECT concat_ws(' ',
	  (SELECT count(*) FROM agentrt.actor), (SELECT count(*) FROM agentrt.event), (SELECT sum(last_seq) FROM agentrt.event_stream),
	  (SELECT count(*) FROM agentrt.idempotency_key), (SELECT count(*) FROM agentrt.mission), (SELECT count(*) FROM agentrt.task),
	  (SELECT count(*) FROM agentrt.task_dependency), (SELECT count(*) FROM agentrt.control), (SELECT count(*) FROM agentrt.artifact))`).Scan(&s))
	return s
}

// wr is a writer as the routes make one: the actor and the token.
func wr(actor string) store.Writer { return store.Writer{Actor: actor, Token: "operator"} }

// legacy writes rows with the import event, which may write any table: the
// fixture for actors the routes do not create (retired ones, odd kinds).
func (w *world) legacy(t *testing.T, stmts ...string) {
	t.Helper()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test"}}); err != nil {
			return err
		}
		for _, q := range stmts {
			if _, err := tx.Exec(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
}

var mission = store.MissionProposal{Title: "m", Goal: "g", Success: []string{"it works"}}

// Missions: the platform numbers them; a thread is registered once, on its
// first write, with the token that wrote; a retry answers from the key; a
// bad proposal writes nothing at all.
func TestProposeMission(t *testing.T) {
	w := newWorld(t)
	first, err := store.ProposeMission(ctx, w.rt, wr("thread:s1"), "k1", mission)
	ok(t, err)
	if first["mission_id"] != "M-3" || first["state"] != "proposed" || first["waiting"] != store.EnvelopePending {
		t.Fatalf("answer %v", first)
	}
	again, err := store.ProposeMission(ctx, w.rt, wr("thread:s1"), "k1", mission)
	ok(t, err)
	same(t, again, first)
	other := mission
	other.Goal = "another goal"
	_, err = store.ProposeMission(ctx, w.rt, wr("thread:s1"), "k1", other)
	is(t, err, store.ErrConflict)
	second, err := store.ProposeMission(ctx, w.rt, wr("thread:s1"), "k2", other)
	ok(t, err)
	if second["mission_id"] != "M-4" {
		t.Fatalf("second mission %v", second)
	}
	if n := w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_type = 'actor_registered' AND body_json->'actors' ? 'thread:s1'
	  AND body_json->>'note' LIKE '%token operator'`); n != 1 {
		t.Fatalf("%d registrations of one thread naming its token", n)
	}
	if n := w.count(t, `SELECT count(*) FROM agentrt.actor WHERE actor_id = 'thread:s1' AND actor_kind = 'thread'
	  AND display_name LIKE '%token operator)'`); n != 1 {
		t.Fatal("the thread is not an actor of kind thread with its token")
	}
	var by, scope, success, budget, tenant string
	ok(t, w.rt.QueryRow(ctx, `SELECT created_by_actor_id, scope_json::text, success_json::text, budget_json::text, tenant
	  FROM agentrt.mission WHERE mission_id = 'M-3'`).Scan(&by, &scope, &success, &budget, &tenant))
	if by != "thread:s1" || scope != `{"max_action": "branch"}` || success != `{"criteria": ["it works"]}` || budget != "{}" || tenant != "dev" {
		t.Fatalf("row: %s %s %s %s %s", by, scope, success, budget, tenant)
	}
	full := mission
	full.Repos, full.MaxAction, full.MaxAttempts, full.MaxWallHours, full.Tenant = []string{"bifrost-platform"}, "none", 40, 48, "business"
	full.Deadline = time.Now().Add(time.Hour)
	out, err := store.ProposeMission(ctx, w.rt, wr("owner"), "k3", full)
	ok(t, err)
	ok(t, w.rt.QueryRow(ctx, `SELECT created_by_actor_id, scope_json::text, budget_json::text, tenant FROM agentrt.mission
	  WHERE mission_id = $1`, out["mission_id"]).Scan(&by, &scope, &budget, &tenant))
	if by != "owner" || scope != `{"repos": ["bifrost-platform"], "max_action": "none"}` || budget != `{"max_attempts": 40, "max_wall_hours": 48}` || tenant != "business" {
		t.Fatalf("row: %s %s %s %s", by, scope, budget, tenant)
	}

	// Actors the routes must refuse: a name of no kind, a thread name
	// registered as something else, a seat that is not one, retired ones.
	w.legacy(t,
		`INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ('thread:x', 'seat', 'x'), ('seat:odd', 'thread', 'odd')`,
		`INSERT INTO agentrt.actor (actor_id, actor_kind, display_name, retired_at) VALUES ('thread:old', 'thread', 'old', now()),
		   ('seat:old', 'seat', 'old', now())`)
	before := w.footprint(t)
	for name, c := range map[string]struct {
		actor, key string
		edit       func(*store.MissionProposal)
	}{
		"no key":          {"thread:s1", "", func(*store.MissionProposal) {}},
		"no title":        {"thread:s1", "b1", func(p *store.MissionProposal) { p.Title = "" }},
		"no goal":         {"thread:s1", "b2", func(p *store.MissionProposal) { p.Goal = "" }},
		"no success":      {"thread:s1", "b3", func(p *store.MissionProposal) { p.Success = nil }},
		"empty criterion": {"thread:s1", "b4", func(p *store.MissionProposal) { p.Success = []string{"a", ""} }},
		"tenant":          {"thread:s1", "b5", func(p *store.MissionProposal) { p.Tenant = "prod" }},
		"external":        {"thread:s1", "b6", func(p *store.MissionProposal) { p.MaxAction = "external" }},
		"past deadline":   {"thread:new", "b7", func(p *store.MissionProposal) { p.Deadline = time.Now().Add(-time.Minute) }},
		"negative budget": {"thread:s1", "b8", func(p *store.MissionProposal) { p.MaxAttempts = -1 }},
		"empty repo":      {"thread:s1", "b9", func(p *store.MissionProposal) { p.Repos = []string{""} }},
		"unknown seat":    {"seat:ghost", "b10", func(*store.MissionProposal) {}},
		"thread a seat":   {"thread:x", "b11", func(*store.MissionProposal) {}},
		"seat a thread":   {"seat:odd", "b12", func(*store.MissionProposal) {}},
		"retired thread":  {"thread:old", "b13", func(*store.MissionProposal) {}},
		"retired seat":    {"seat:old", "b14", func(*store.MissionProposal) {}},
		"no kind":         {"someone", "b15", func(*store.MissionProposal) {}},
	} {
		p := mission
		c.edit(&p)
		if _, err := store.ProposeMission(ctx, w.rt, wr(c.actor), c.key, p); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if after := w.footprint(t); after != before {
		t.Fatalf("refused proposals left something: %s -> %s", before, after)
	}
	w.chainHolds(t, "system")
}

// Review 175 A4: a proposal made before its deadline, retried after it, gets
// its first answer; the same key with another deadline is a conflict; a new
// proposal past its deadline is refused and writes nothing.
func TestMissionDeadlineRetry(t *testing.T) {
	w := newWorld(t)
	p := mission
	p.Deadline = time.Now().Add(300 * time.Millisecond)
	first, err := store.ProposeMission(ctx, w.rt, wr("thread:s1"), "k1", p)
	ok(t, err)
	time.Sleep(400 * time.Millisecond)
	again, err := store.ProposeMission(ctx, w.rt, wr("thread:s1"), "k1", p)
	ok(t, err)
	same(t, again, first)
	moved := p
	moved.Deadline = time.Now().Add(time.Hour)
	_, err = store.ProposeMission(ctx, w.rt, wr("thread:s1"), "k1", moved)
	is(t, err, store.ErrConflict)
	before := w.footprint(t)
	_, err = store.ProposeMission(ctx, w.rt, wr("thread:s2"), "k2", p)
	is(t, err, store.ErrInvalid)
	if after := w.footprint(t); after != before {
		t.Fatalf("a proposal past its deadline left something: %s -> %s", before, after)
	}
}

// Review 175 B3: thread registrations are bounded per day; threads already
// registered go on writing.
func TestThreadRegistrationBound(t *testing.T) {
	w := newWorld(t)
	_, err := store.ProposeMission(ctx, w.rt, wr("thread:known"), "k1", mission)
	ok(t, err)
	w.legacy(t, fmt.Sprintf(`INSERT INTO agentrt.actor (actor_id, actor_kind, display_name)
	  SELECT 'thread:bulk' || i, 'thread', 'bulk' FROM generate_series(1, %d) i`, store.ThreadsPerDay-1))
	before := w.footprint(t)
	_, err = store.ProposeMission(ctx, w.rt, wr("thread:one-more"), "k1", mission)
	if !errors.Is(err, store.ErrInvalid) || !strings.Contains(err.Error(), "threads were registered") {
		t.Fatalf("a thread past the day's bound: %v", err)
	}
	if after := w.footprint(t); after != before {
		t.Fatalf("a refused registration left something: %s -> %s", before, after)
	}
	_, err = store.ProposeMission(ctx, w.rt, wr("thread:known"), "k2", mission)
	ok(t, err)
}

// Two proposals at once (threads already registered, so nothing else orders
// them): the second runs while the first holds its number; with the number
// lock it waits, and the two get two numbers (without it the two would take
// the same number and one would fail).
func TestMissionNumberLock(t *testing.T) {
	w := newWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	for _, th := range []string{"thread:a", "thread:b"} {
		_, err := store.ProposeMission(ctx, pool, wr(th), "first", mission)
		ok(t, err)
	}
	hold, reached := make(chan struct{}), make(chan struct{})
	var first atomic.Bool
	var release sync.Once
	free := func() { release.Do(func() { close(hold) }) }
	defer free()
	defer store.SetHook(func(point, _ string) {
		if point == "numbered" && first.CompareAndSwap(false, true) {
			close(reached)
			<-hold
		}
	})()
	got := make(chan any, 2)
	propose := func(th string) {
		out, err := store.ProposeMission(ctx, pool, wr(th), "second", mission)
		if err != nil {
			got <- err
			return
		}
		got <- out["mission_id"]
	}
	go propose("thread:a")
	<-reached
	go propose("thread:b")
	// Until the second waits on the number lock, or (without it) is done.
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if w.count(t, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'
		  AND position('742108' in query) > 0`) > 0 || len(got) > 0 {
			break
		}
	}
	free()
	a, b := <-got, <-got
	for _, r := range []any{a, b} {
		if err, isErr := r.(error); isErr {
			t.Fatalf("a proposal at the same time failed: %v", err)
		}
	}
	if a == b {
		t.Fatalf("two proposals took one number: %v", a)
	}
}

func proposal(ref, spec string, deps ...string) store.TaskProposal {
	return store.TaskProposal{Ref: ref, PlanNode: s1, Role: "implement", Class: "go", Title: ref, SpecArtifact: spec,
		Effect: "branch", Repo: "bifrost-platform", BaseCommit: commitA, DependsOn: deps}
}

func offPlan(p store.TaskProposal) store.TaskProposal {
	p.PlanNode, p.OffPlanReason = "", "test"
	return p
}

func taskIDs(out map[string]any) map[string]string {
	id := map[string]string{}
	for _, e := range out["tasks"].([]any) {
		m := e.(map[string]any)
		id[m["ref"].(string)] = m["task_id"].(string)
	}
	return id
}

// Tasks: a batch is all or nothing, every refusal leaves nothing (T32
// through the store); dependencies are refs of the batch or tasks of the
// mission.
func TestProposeTasks(t *testing.T) {
	w := newKernelWorld(t)
	existing := w.ready(t, store.Task{})
	b := proposal("b", w.spec, "a")
	b.PlanNode, b.OffPlanReason = "", "found while doing a"
	c := proposal("c", w.spec, existing)
	c.Role, c.Core, c.Needs = "plan", true, []string{"git_commit"}
	batch := []store.TaskProposal{proposal("a", w.spec), b, c}
	out, err := store.ProposeTasks(ctx, w.rt, wr("thread:s1"), "k1", "M-1", batch)
	ok(t, err)
	id := taskIDs(out)
	if len(id) != 3 {
		t.Fatalf("answer %v", out)
	}
	for ref, task := range id {
		if s := w.state(t, "task", task); s != "proposed" {
			t.Fatalf("%s is %s", ref, s)
		}
	}
	again, err := store.ProposeTasks(ctx, w.rt, wr("thread:s1"), "k1", "M-1", batch)
	ok(t, err)
	same(t, again, out)
	if n := w.count(t, `SELECT count(*) FROM agentrt.task_dependency WHERE (task_id, depends_on_task_id) IN (($1, $2), ($3, $4))`,
		id["b"], id["a"], id["c"], existing); n != 2 {
		t.Fatalf("%d of 2 dependencies", n)
	}
	var offPlanRow, core bool
	var by, needs string
	ok(t, w.rt.QueryRow(ctx, `SELECT off_plan, created_by_actor_id FROM agentrt.task WHERE task_id = $1`, id["b"]).Scan(&offPlanRow, &by))
	ok(t, w.rt.QueryRow(ctx, `SELECT core, needs_json::text FROM agentrt.task WHERE task_id = $1`, id["c"]).Scan(&core, &needs))
	if !offPlanRow || by != "thread:s1" || !core || needs != `["git_commit"]` {
		t.Fatalf("rows: off_plan %v by %s core %v needs %s", offPlanRow, by, core, needs)
	}

	report := w.artifact(t, "report", "# not a spec\n")
	other, err := store.ProposeTasks(ctx, w.rt, wr("thread:s1"), "k2", "M-2", []store.TaskProposal{offPlan(proposal("x", w.spec))})
	ok(t, err)
	otherTask := taskIDs(other)["x"]
	const s4 = "R2:2.1-4"
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, cerr := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: s4, Parent: item, Kind: "step", Label: "2.1-4",
			Title: "s4", MissionID: "M-2", StepKind: "agent", Ordinal: 3, Root: root})
		return cerr
	}))
	before := w.footprint(t)
	edit := func(f func(*store.TaskProposal)) []store.TaskProposal {
		p := proposal("a", w.spec)
		f(&p)
		return []store.TaskProposal{proposal("ok", w.spec), p}
	}
	for name, c := range map[string]struct {
		batch   []store.TaskProposal
		invalid bool
	}{
		"empty":     {nil, true},
		"no ref":    {edit(func(p *store.TaskProposal) { p.Ref = "" }), true},
		"ref twice": {edit(func(p *store.TaskProposal) { p.Ref = "ok" }), true},
		// A review that the database would take (its subject on the same
		// step): only the role rule refuses it (review 175 B6).
		"review":           {edit(func(p *store.TaskProposal) { p.Role, p.Subject = "review", existing }), true},
		"verify":           {edit(func(p *store.TaskProposal) { p.Role, p.Subject = "verify", existing }), true},
		"no title":         {edit(func(p *store.TaskProposal) { p.Title = "" }), true},
		"step and reason":  {edit(func(p *store.TaskProposal) { p.OffPlanReason = "x" }), true},
		"neither":          {edit(func(p *store.TaskProposal) { p.PlanNode = "" }), true},
		"effect":           {edit(func(p *store.TaskProposal) { p.Effect = "deploy" }), true},
		"above max action": {edit(func(p *store.TaskProposal) { p.Effect = "external" }), true},
		"spec missing":     {edit(func(p *store.TaskProposal) { p.SpecArtifact = noSuch }), true},
		"spec a report":    {edit(func(p *store.TaskProposal) { p.SpecArtifact = report }), true},
		"unknown ref":      {edit(func(p *store.TaskProposal) { p.DependsOn = []string{"nope"} }), true},
		"other mission":    {edit(func(p *store.TaskProposal) { p.DependsOn = []string{otherTask} }), true},
		"negative limit":   {edit(func(p *store.TaskProposal) { p.HardLimit = -time.Second }), true},
		"limit too long":   {edit(func(p *store.TaskProposal) { p.HardLimit = store.MaxHardLimit + time.Second }), true},
		"part of a second": {edit(func(p *store.TaskProposal) { p.HardLimit = 1500 * time.Millisecond }), true},
		// The database's guards (T32): an item is not a step; a step of
		// another mission; a cycle; a commit that is not one.
		"on an item":             {edit(func(p *store.TaskProposal) { p.PlanNode = item }), false},
		"another mission's step": {edit(func(p *store.TaskProposal) { p.PlanNode = s4 }), false},
		"cycle": {func() []store.TaskProposal {
			return []store.TaskProposal{proposal("x", w.spec, "y"), proposal("y", w.spec, "x")}
		}(), false},
		"base commit": {edit(func(p *store.TaskProposal) { p.BaseCommit = "main" }), false},
	} {
		_, perr := store.ProposeTasks(ctx, w.rt, wr("thread:fresh"), "r-"+name, "M-1", c.batch)
		if perr == nil || c.invalid != errors.Is(perr, store.ErrInvalid) {
			t.Errorf("%s: err = %v", name, perr)
		}
	}
	if after := w.footprint(t); after != before {
		t.Fatalf("refused batches left something: %s -> %s", before, after)
	}
	// A stopped mission takes no task that would otherwise fit (review 175 B6).
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-2", "stopped", "test") }))
	before = w.footprint(t)
	_, err = store.ProposeTasks(ctx, w.rt, wr("thread:s1"), "k3", "M-2", []store.TaskProposal{offPlan(proposal("a", w.spec))})
	is(t, err, store.ErrState)
	_, err = store.ProposeTasks(ctx, w.rt, wr("thread:s1"), "k4", "M-99", []store.TaskProposal{offPlan(proposal("a", w.spec))})
	is(t, err, pgx.ErrNoRows)
	if after := w.footprint(t); after != before {
		t.Fatalf("refused batches left something: %s -> %s", before, after)
	}
	w.chainHolds(t, "mission:M-1")
}

// Dispatch: a task of an active mission whose dependencies are all accepted
// becomes ready with its route; one waiting stays proposed; one whose
// dependency was cancelled is blocked; tasks of a mission not active wait.
func TestDispatch(t *testing.T) {
	w := newKernelWorld(t)
	out, err := store.ProposeTasks(ctx, w.rt, wr("thread:s1"), "k1", "M-1",
		[]store.TaskProposal{proposal("a", w.spec), proposal("b", w.spec, "a"), proposal("d", w.spec), proposal("c", w.spec, "d")})
	ok(t, err)
	id := taskIDs(out)
	idle, err := store.ProposeTasks(ctx, w.rt, wr("thread:s1"), "k2", "M-2", []store.TaskProposal{offPlan(proposal("e", w.spec))})
	ok(t, err)
	e := taskIDs(idle)["e"]
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveTaskForTest(ctx, tx, "controller", id["d"], "cancelled") }))

	moved, err := store.Dispatch(ctx, w.rt)
	ok(t, err)
	if moved != 2 {
		t.Fatalf("moved %d; want a ready and c blocked", moved)
	}
	for ref, want := range map[string]string{"a": "ready", "b": "proposed", "c": "blocked", "d": "cancelled"} {
		if s := w.state(t, "task", id[ref]); s != want {
			t.Errorf("%s is %s; want %s", ref, s, want)
		}
	}
	if s := w.state(t, "task", e); s != "proposed" {
		t.Errorf("a task of a proposed mission is %s", s)
	}
	if n := w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_type = 'route_decided' AND task_id = $1`, id["a"]); n != 1 {
		t.Errorf("%d routes for the ready task", n)
	}
	if n := w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_type = 'task_state_changed' AND task_id = $1
	  AND body_json->>'reason' = $2`, id["c"], "dependency "+id["d"]+" is cancelled"); n != 1 {
		t.Error("the block does not say which dependency")
	}
	if moved, err = store.Dispatch(ctx, w.rt); err != nil || moved != 0 {
		t.Fatalf("a second pass moved %d (%v)", moved, err)
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		for _, to := range []string{"leased", "running", "submitted", "accepted"} {
			if merr := store.MoveTaskForTest(ctx, tx, "controller", id["a"], to); merr != nil {
				return merr
			}
		}
		return nil
	}))
	if moved, err = store.Dispatch(ctx, w.rt); err != nil || moved != 1 || w.state(t, "task", id["b"]) != "ready" {
		t.Fatalf("after a was accepted: moved %d (%v), b %s", moved, err, w.state(t, "task", id["b"]))
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-2", "active", "test") }))
	if moved, err = store.Dispatch(ctx, w.rt); err != nil || moved != 1 || w.state(t, "task", e) != "ready" {
		t.Fatalf("once M-2 is active: moved %d (%v), e %s", moved, err, w.state(t, "task", e))
	}
	w.chainHolds(t, "mission:M-1")
}

// Dispatch re-reads the mission under its lock: a mission paused after the
// pass listed its tasks gets neither a task readied nor one blocked (review
// 175 B2).
func TestDispatchRechecksMission(t *testing.T) {
	w := newKernelWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	out, err := store.ProposeTasks(ctx, pool, wr("thread:s1"), "k1", "M-1",
		[]store.TaskProposal{proposal("free", w.spec), proposal("d", w.spec), proposal("c", w.spec, "d")})
	ok(t, err)
	id := taskIDs(out)
	ok(t, store.InTx(ctx, pool, func(tx pgx.Tx) error { return store.MoveTaskForTest(ctx, tx, "controller", id["d"], "cancelled") }))
	var once sync.Once
	defer store.SetHook(func(point, _ string) {
		if point == "dispatching" {
			once.Do(func() {
				ok(t, store.InTx(ctx, pool, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-1", "paused", "test") }))
			})
		}
	})()
	moved, err := store.Dispatch(ctx, pool)
	if err != nil || moved != 0 {
		t.Fatalf("tasks of a mission paused mid-pass: moved %d (%v)", moved, err)
	}
	for _, ref := range []string{"free", "c"} {
		if n := w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_type = 'task_state_changed' AND task_id = $1`, id[ref]); n != 0 {
			t.Errorf("%s of a paused mission moved", ref)
		}
	}
}

// Review 175 A3: tasks that wait never keep later ones from being looked
// at: with more waiting tasks than a page, a later task with nothing to wait
// for is ready after one pass.
func TestDispatchPages(t *testing.T) {
	w := newKernelWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	gate, err := store.ProposeTasks(ctx, pool, wr("thread:s1"), "gate", "M-1", []store.TaskProposal{proposal("gate", w.spec)})
	ok(t, err)
	g := taskIDs(gate)["gate"]
	for b := 0; b < 11; b++ { // 550 tasks that wait for the gate
		batch := make([]store.TaskProposal, 0, store.MaxBatch)
		for i := range store.MaxBatch {
			batch = append(batch, proposal(fmt.Sprintf("w%d", i), w.spec, g))
		}
		_, err = store.ProposeTasks(ctx, pool, wr("thread:s1"), fmt.Sprintf("b%d", b), "M-1", batch)
		ok(t, err)
	}
	last, err := store.ProposeTasks(ctx, pool, wr("thread:s1"), "last", "M-1", []store.TaskProposal{proposal("last", w.spec)})
	ok(t, err)
	moved, err := store.Dispatch(ctx, pool)
	ok(t, err)
	if s := w.state(t, "task", taskIDs(last)["last"]); s != "ready" || moved != 2 {
		t.Fatalf("after one pass the last task is %s, %d moved; want the gate and it", s, moved)
	}
}

// Review 175 A1: a dependency cannot slip past Dispatch. Added after the
// task was readied, it is refused; added while Dispatch waits for the task,
// Dispatch sees it and leaves the task proposed.
func TestDependencyAgainstDispatch(t *testing.T) {
	w := newKernelWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	// x first, so it is the first task Dispatch looks at.
	first, err := store.ProposeTasks(ctx, pool, wr("thread:s1"), "k0", "M-1", []store.TaskProposal{proposal("x", w.spec)})
	ok(t, err)
	out, err := store.ProposeTasks(ctx, pool, wr("thread:s1"), "k1", "M-1",
		[]store.TaskProposal{proposal("t", w.spec), proposal("u", w.spec, "v"), proposal("v", w.spec)})
	ok(t, err)
	id := taskIDs(out)
	id["x"] = taskIDs(first)["x"]
	ok(t, store.InTx(ctx, pool, func(tx pgx.Tx) error { return store.MoveTaskForTest(ctx, tx, "controller", id["t"], "ready") }))
	err = store.InTx(ctx, pool, func(tx pgx.Tx) error {
		_, aerr := store.AddDependency(ctx, tx, "controller", "M-1", id["t"], id["u"])
		return aerr
	})
	is(t, err, store.ErrState)

	// x: an edge to u (which waits for v) added in an open transaction;
	// Dispatch starts meanwhile and is held until it commits, then sees
	// the edge and leaves x proposed.
	tx, err := pool.Begin(ctx)
	ok(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = store.AddDependency(ctx, tx, "controller", "M-1", id["x"], id["u"])
	ok(t, err)
	done := make(chan error, 1)
	go func() {
		_, derr := store.Dispatch(ctx, pool)
		done <- derr
	}()
	w.waitFor(t, "agentrt.", time.Now().Add(5*time.Second))
	ok(t, tx.Commit(ctx))
	ok(t, <-done)
	if s := w.state(t, "task", id["x"]); s != "proposed" {
		t.Fatalf("x, which now waits for u, is %s", s)
	}
}

// Review 175 A2: a batch with dependencies and another dependency of the
// same mission at once do not deadlock: the batch holds the graph lock
// before its first event, so the other waits for it and both commit.
func TestBatchAgainstDependency(t *testing.T) {
	w := newKernelWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	base, err := store.ProposeTasks(ctx, pool, wr("thread:s1"), "k0", "M-1",
		[]store.TaskProposal{proposal("p", w.spec), proposal("q", w.spec)})
	ok(t, err)
	id := taskIDs(base)
	hold, reached := make(chan struct{}), make(chan struct{})
	var first atomic.Bool
	var release sync.Once
	free := func() { release.Do(func() { close(hold) }) }
	defer free()
	defer store.SetHook(func(point, _ string) {
		if point == "proposed" && first.CompareAndSwap(false, true) {
			close(reached)
			<-hold
		}
	})()
	batch := make(chan error, 1)
	go func() {
		_, berr := store.ProposeTasks(ctx, pool, wr("thread:s1"), "k1", "M-1",
			[]store.TaskProposal{proposal("n1", w.spec, id["p"]), proposal("n2", w.spec, "n1")})
		batch <- berr
	}()
	<-reached
	edge := make(chan error, 1)
	go func() {
		edge <- store.InTx(ctx, pool, func(tx pgx.Tx) error {
			_, aerr := store.AddDependency(ctx, tx, "controller", "M-1", id["q"], id["p"])
			return aerr
		})
	}()
	// The dependency waits on a lock: with the batch holding the graph lock
	// that is the graph lock; were the batch to take it late, it would be
	// the mission's stream, and releasing the batch would deadlock.
	w.waitFor(t, "", time.Now().Add(5*time.Second))
	free()
	if berr, eerr := <-batch, <-edge; berr != nil || eerr != nil {
		t.Fatalf("batch: %v; dependency: %v", berr, eerr)
	}
}
