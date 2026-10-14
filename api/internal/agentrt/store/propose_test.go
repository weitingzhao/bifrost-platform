package store_test

import (
	"errors"
	"fmt"
	"sync"
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

var mission = store.MissionProposal{Title: "m", Goal: "g", Success: []string{"it works"}}

// Missions: the platform numbers them; a thread is registered once, on its
// first write; a retry answers from the key; a bad proposal writes nothing.
func TestProposeMission(t *testing.T) {
	w := newWorld(t)
	first, err := store.ProposeMission(ctx, w.rt, "thread:s1", "k1", mission)
	ok(t, err)
	if first["mission_id"] != "M-3" || first["state"] != "proposed" || first["waiting"] != store.EnvelopePending {
		t.Fatalf("answer %v", first)
	}
	again, err := store.ProposeMission(ctx, w.rt, "thread:s1", "k1", mission)
	ok(t, err)
	same(t, again, first)
	other := mission
	other.Goal = "another goal"
	_, err = store.ProposeMission(ctx, w.rt, "thread:s1", "k1", other)
	is(t, err, store.ErrConflict)
	second, err := store.ProposeMission(ctx, w.rt, "thread:s1", "k2", other)
	ok(t, err)
	if second["mission_id"] != "M-4" {
		t.Fatalf("second mission %v", second)
	}
	if n := w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_type = 'actor_registered' AND body_json->'actors' ? 'thread:s1'`); n != 1 {
		t.Fatalf("%d registrations of one thread", n)
	}
	if kind := w.count(t, `SELECT count(*) FROM agentrt.actor WHERE actor_id = 'thread:s1' AND actor_kind = 'thread'`); kind != 1 {
		t.Fatal("the thread is not an actor of kind thread")
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
	out, err := store.ProposeMission(ctx, w.rt, "owner", "k3", full)
	ok(t, err)
	ok(t, w.rt.QueryRow(ctx, `SELECT created_by_actor_id, scope_json::text, budget_json::text, tenant FROM agentrt.mission
	  WHERE mission_id = $1`, out["mission_id"]).Scan(&by, &scope, &budget, &tenant))
	if by != "owner" || scope != `{"repos": ["bifrost-platform"], "max_action": "none"}` || budget != `{"max_attempts": 40, "max_wall_hours": 48}` || tenant != "business" {
		t.Fatalf("row: %s %s %s %s", by, scope, budget, tenant)
	}

	before := w.count(t, `SELECT count(*) FROM agentrt.mission`)
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
		"past deadline":   {"thread:s1", "b7", func(p *store.MissionProposal) { p.Deadline = time.Now().Add(-time.Minute) }},
		"negative budget": {"thread:s1", "b8", func(p *store.MissionProposal) { p.MaxAttempts = -1 }},
		"empty repo":      {"thread:s1", "b9", func(p *store.MissionProposal) { p.Repos = []string{""} }},
		"unknown seat":    {"seat:ghost", "b10", func(*store.MissionProposal) {}},
		"seat as thread":  {"thread:x", "b11", func(*store.MissionProposal) {}},
	} {
		p := mission
		c.edit(&p)
		if name == "seat as thread" {
			// An actor named thread: that is registered as something else.
			ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
				if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "actor_registered", Actor: "controller",
					Body: map[string]any{"actors": []string{"thread:x"}, "note": "test"}}); err != nil {
					return err
				}
				_, err := tx.Exec(ctx, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ('thread:x', 'seat', 'x')`)
				return err
			}))
		}
		if _, err := store.ProposeMission(ctx, w.rt, c.actor, c.key, p); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if after := w.count(t, `SELECT count(*) FROM agentrt.mission`); after != before {
		t.Fatalf("refused proposals wrote %d missions", after-before)
	}

	// Proposals at once get one number each.
	pool := w.db.Pool(t, w.db.Runtime)
	var wg sync.WaitGroup
	ids := make(chan any, 8)
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := store.ProposeMission(ctx, pool, fmt.Sprintf("thread:c%d", i), "k", mission)
			if err != nil {
				ids <- err
				return
			}
			ids <- out["mission_id"]
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[any]bool{}
	for id := range ids {
		if _, isErr := id.(error); isErr || seen[id] {
			t.Fatalf("concurrent proposal: %v", id)
		}
		seen[id] = true
	}
	w.chainHolds(t, "system")
}

func proposal(ref, spec string, deps ...string) store.TaskProposal {
	return store.TaskProposal{Ref: ref, PlanNode: s1, Role: "implement", Class: "go", Title: ref, SpecArtifact: spec,
		Effect: "branch", Repo: "bifrost-platform", BaseCommit: commitA, DependsOn: deps}
}

// Tasks: a batch is all or nothing, every refusal leaves no row (T32 through
// the store); dependencies are refs of the batch or tasks of the mission.
func TestProposeTasks(t *testing.T) {
	w := newKernelWorld(t)
	existing := w.ready(t, store.Task{})
	b := proposal("b", w.spec, "a")
	b.PlanNode, b.OffPlanReason = "", "found while doing a"
	c := proposal("c", w.spec, existing)
	c.Role, c.Core, c.Needs = "plan", true, []string{"git_commit"}
	batch := []store.TaskProposal{proposal("a", w.spec), b, c}
	out, err := store.ProposeTasks(ctx, w.rt, "thread:s1", "k1", "M-1", batch)
	ok(t, err)
	got := out["tasks"].([]any)
	if len(got) != 3 {
		t.Fatalf("answer %v", out)
	}
	id := map[string]string{}
	for _, e := range got {
		m := e.(map[string]any)
		id[m["ref"].(string)] = m["task_id"].(string)
		if s := w.state(t, "task", id[m["ref"].(string)]); s != "proposed" {
			t.Fatalf("%v is %s", m, s)
		}
	}
	again, err := store.ProposeTasks(ctx, w.rt, "thread:s1", "k1", "M-1", batch)
	ok(t, err)
	same(t, again, out)
	if n := w.count(t, `SELECT count(*) FROM agentrt.task_dependency WHERE (task_id, depends_on_task_id) IN (($1, $2), ($3, $4))`,
		id["b"], id["a"], id["c"], existing); n != 2 {
		t.Fatalf("%d of 2 dependencies", n)
	}
	var offPlan, core bool
	var by, needs string
	ok(t, w.rt.QueryRow(ctx, `SELECT off_plan, created_by_actor_id FROM agentrt.task WHERE task_id = $1`, id["b"]).Scan(&offPlan, &by))
	ok(t, w.rt.QueryRow(ctx, `SELECT core, needs_json::text FROM agentrt.task WHERE task_id = $1`, id["c"]).Scan(&core, &needs))
	if !offPlan || by != "thread:s1" || !core || needs != `["git_commit"]` {
		t.Fatalf("rows: off_plan %v by %s core %v needs %s", offPlan, by, core, needs)
	}

	report := w.artifact(t, "report", "# not a spec\n")
	other, err := store.ProposeTasks(ctx, w.rt, "thread:s1", "k2", "M-2", []store.TaskProposal{func() store.TaskProposal {
		p := proposal("x", w.spec)
		p.PlanNode, p.OffPlanReason = "", "other mission"
		return p
	}()})
	ok(t, err)
	otherTask := other["tasks"].([]any)[0].(map[string]any)["task_id"].(string)
	const s4 = "R2:2.1-4"
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, cerr := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: s4, Parent: item, Kind: "step", Label: "2.1-4",
			Title: "s4", MissionID: "M-2", StepKind: "agent", Ordinal: 3, Root: root})
		return cerr
	}))
	before := w.count(t, `SELECT count(*) FROM agentrt.task`)
	deps := w.count(t, `SELECT count(*) FROM agentrt.task_dependency`)
	edit := func(f func(*store.TaskProposal)) []store.TaskProposal {
		p := proposal("a", w.spec)
		f(&p)
		return []store.TaskProposal{proposal("ok", w.spec), p}
	}
	for name, c := range map[string]struct {
		batch   []store.TaskProposal
		invalid bool
	}{
		"empty":            {nil, true},
		"no ref":           {edit(func(p *store.TaskProposal) { p.Ref = "" }), true},
		"ref twice":        {edit(func(p *store.TaskProposal) { p.Ref = "ok" }), true},
		"review":           {edit(func(p *store.TaskProposal) { p.Role = "review" }), true},
		"verify":           {edit(func(p *store.TaskProposal) { p.Role = "verify" }), true},
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
		// The database's guards (T32): an item is not a step; a step of
		// another mission; a cycle; a commit that is not one.
		"on an item":             {edit(func(p *store.TaskProposal) { p.PlanNode = item }), false},
		"another mission's step": {edit(func(p *store.TaskProposal) { p.PlanNode = s4 }), false},
		"cycle": {func() []store.TaskProposal {
			x, y := proposal("x", w.spec, "y"), proposal("y", w.spec, "x")
			return []store.TaskProposal{x, y}
		}(), false},
		"base commit": {edit(func(p *store.TaskProposal) { p.BaseCommit = "main" }), false},
	} {
		_, perr := store.ProposeTasks(ctx, w.rt, "thread:s1", "r-"+name, "M-1", c.batch)
		if perr == nil || c.invalid != errors.Is(perr, store.ErrInvalid) {
			t.Errorf("%s: err = %v", name, perr)
		}
	}
	if n, d := w.count(t, `SELECT count(*) FROM agentrt.task`), w.count(t, `SELECT count(*) FROM agentrt.task_dependency`); n != before || d != deps {
		t.Fatalf("refused batches wrote %d tasks, %d dependencies", n-before, d-deps)
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-2", "stopped", "test") }))
	_, err = store.ProposeTasks(ctx, w.rt, "thread:s1", "k3", "M-2", []store.TaskProposal{proposal("a", w.spec)})
	is(t, err, store.ErrState)
	_, err = store.ProposeTasks(ctx, w.rt, "thread:s1", "k4", "M-99", []store.TaskProposal{proposal("a", w.spec)})
	is(t, err, pgx.ErrNoRows)
	w.chainHolds(t, "mission:M-1")
}

// Dispatch: a task of an active mission whose dependencies are all accepted
// becomes ready with its route; one waiting stays proposed; one whose
// dependency was cancelled is blocked; tasks of a mission not active wait.
func TestDispatch(t *testing.T) {
	w := newKernelWorld(t)
	off := func(ref string, deps ...string) store.TaskProposal {
		p := proposal(ref, w.spec, deps...)
		p.PlanNode, p.OffPlanReason = "", "test"
		return p
	}
	out, err := store.ProposeTasks(ctx, w.rt, "thread:s1", "k1", "M-1",
		[]store.TaskProposal{proposal("a", w.spec), proposal("b", w.spec, "a"), proposal("d", w.spec), proposal("c", w.spec, "d")})
	ok(t, err)
	id := map[string]string{}
	for _, e := range out["tasks"].([]any) {
		m := e.(map[string]any)
		id[m["ref"].(string)] = m["task_id"].(string)
	}
	idle, err := store.ProposeTasks(ctx, w.rt, "thread:s1", "k2", "M-2", []store.TaskProposal{off("e")})
	ok(t, err)
	e := idle["tasks"].([]any)[0].(map[string]any)["task_id"].(string)
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

// Two proposals at once (threads already registered, so nothing else orders
// them): the second waits on the number lock while the first holds its
// number, and they get two numbers.
func TestMissionNumberLock(t *testing.T) {
	w := newWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	for _, th := range []string{"thread:a", "thread:b"} {
		_, err := store.ProposeMission(ctx, pool, th, "first", mission)
		ok(t, err)
	}
	hold, reached := make(chan struct{}), make(chan struct{})
	var once, release sync.Once
	free := func() { release.Do(func() { close(hold) }) }
	defer free()
	defer store.SetHook(func(point, _ string) {
		if point == "numbered" {
			once.Do(func() {
				close(reached)
				<-hold
			})
		}
	})()
	got := make(chan any, 2)
	propose := func(th string) {
		out, err := store.ProposeMission(ctx, pool, th, "second", mission)
		if err != nil {
			got <- err
			return
		}
		got <- out["mission_id"]
	}
	go propose("thread:a")
	<-reached
	go propose("thread:b")
	w.waitFor(t, "pg_advisory_xact_lock(742108", time.Now().Add(5*time.Second))
	free()
	first, second := <-got, <-got
	if _, isErr := first.(error); isErr || first == second {
		t.Fatalf("two proposals at once: %v and %v", first, second)
	}
	if _, isErr := second.(error); isErr {
		t.Fatalf("the second proposal: %v", second)
	}
}

// Dispatch re-reads the mission under its lock: a mission paused after the
// pass listed its task does not get the task readied.
func TestDispatchRechecksMission(t *testing.T) {
	w := newKernelWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	_, err := store.ProposeTasks(ctx, pool, "thread:s1", "k1", "M-1", []store.TaskProposal{proposal("a", w.spec)})
	ok(t, err)
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
		t.Fatalf("a task of a mission paused mid-pass: moved %d (%v)", moved, err)
	}
	if n := w.count(t, `SELECT count(*) FROM agentrt.task WHERE mission_id = 'M-1' AND state = 'ready'`); n != 0 {
		t.Fatalf("%d tasks of a paused mission are ready", n)
	}
}
