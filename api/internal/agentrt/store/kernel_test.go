package store_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

var (
	codex  = store.Tool{Executor: "exec:codex", Provider: "openai", Model: "gpt-x"}
	claude = store.Tool{Executor: "exec:claude", Provider: "anthropic", Model: "opus"}
	cursor = store.Tool{Executor: "exec:cursor", Provider: "cursor-mix"}
)

const (
	commitA = "2333e00753f91c74c8b2f4d693a803f8e89e7892"
	commitB = "0b9e1f3c7a1d2e4f5a6b7c8d9e0f1a2b3c4d5e6f"
	noSuch  = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
)

// newKernelWorld is a world with mission M-1 active, hosts h1–h8, and every
// tool the tests use a candidate for every job.
func newKernelWorld(t *testing.T) *world {
	t.Helper()
	ctx := ctxOf(t)
	w := newWorld(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-1", "active", "test") }))
	for i := 1; i <= 8; i++ {
		ok(t, store.RegisterHost(ctx, w.rt, "owner", fmt.Sprintf("h%d", i), "dev"))
	}
	_, err := w.rt.Exec(ctx, agentrttest.DispatchRules)
	ok(t, err)
	// The seed's Owner facts as if he had signed capability-facts: core
	// work needs its only facts to grant.
	w.legacy(t, agentrttest.OwnerFactsRule, agentrttest.BindOwnerFacts)
	w.jobs(t, "active", "*", codex, claude, cursor, deterministic, store.Tool{Executor: "exec:claude", Provider: "other"},
		store.Tool{Executor: "exec:codex", Provider: "other"}, store.Tool{Executor: "exec:codex", Provider: "cursor-mix"},
		store.Tool{Executor: "exec:claude", Provider: "cursor-mix"})
	return w
}

// jobs writes the jobs and makes each tool a candidate for every job with
// the given status and model pattern. The Owner's signed initial rule writes
// these (O19); a test writes them with the import event, under a rule of its
// own.
func (w *world) jobs(t *testing.T, status, pattern string, tools ...store.Tool) {
	t.Helper()
	ctx := ctxOf(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if err := store.LockCapabilities(ctx, tx); err != nil {
			return err
		}
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: jobs and candidates"}}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, agentrttest.TestJobRule); err != nil {
			return err
		}
		for _, job := range []string{"planner", "implementer", "verifier", "reviewer", "chief_of_staff"} {
			if _, err := tx.Exec(ctx, `INSERT INTO agentrt.job (job_id, tenant, rule_id, rule_version) VALUES ($1, 'dev', 'job:test', 1)
			  ON CONFLICT DO NOTHING`, job); err != nil {
				return err
			}
			for _, tl := range tools {
				if _, err := tx.Exec(ctx, `INSERT INTO agentrt.job_candidate (job_candidate_id, job_id, executor_actor_id, model_provider,
				  model_pattern, status, rule_id, rule_version) VALUES ($1, $2, $3, $4, $5, $6, 'job:test', 1)`, job+"/"+tl.Executor+"/"+tl.Provider+"/"+pattern, job,
					tl.Executor, tl.Provider, pattern, status); err != nil {
					return err
				}
			}
		}
		return nil
	}))
}

// ready proposes a task, defaults filled in, and moves it to ready.
func (w *world) ready(t *testing.T, task store.Task) string {
	t.Helper()
	ctx := ctxOf(t)
	if task.ID == "" {
		task.ID = store.NewTaskID()
	}
	if task.MissionID == "" {
		task.MissionID = "M-1"
	}
	if task.PlanNode == "" && task.OffPlanReason == "" {
		task.PlanNode = s1
	}
	if task.Role == "" {
		task.Role = "implement"
	}
	if task.Effect == "" {
		task.Effect = "branch"
	}
	if task.Repo == "" {
		task.Repo = "bifrost-platform"
	}
	task.Class, task.Title, task.SpecArtifact = "go", "t", w.spec
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.ProposeTask(ctx, tx, "controller", task); err != nil {
			return err
		}
		return store.MarkReady(ctx, tx, "controller", task.ID)
	}))
	return task.ID
}

func (w *world) claim(t *testing.T, host string, ttl time.Duration, tools ...store.Tool) *store.Claim {
	t.Helper()
	ctx := ctxOf(t)
	c, err := store.ClaimFor(ctx, w.rt, store.ClaimRequest{HostID: host, FreeSlots: 1, Tools: tools}, ttl)
	ok(t, err)
	return c
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

func fence(c *store.Claim, host string) store.Fence {
	return store.Fence{AttemptID: c.AttemptID, HostID: host, Epoch: c.Epoch}
}

func (w *world) state(t *testing.T, table, id string) string {
	t.Helper()
	ctx := ctxOf(t)
	var s string
	ok(t, w.rt.QueryRow(ctx, fmt.Sprintf(`SELECT state FROM agentrt.%[1]s WHERE %[1]s_id = $1`, table), id).Scan(&s))
	return s
}

func (w *world) events(t *testing.T, typ, attempt string) int {
	t.Helper()
	var n int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = $1 AND attempt_id = $2`, typ, attempt).Scan(&n))
	return n
}

func (w *world) artifact(t *testing.T, kind, body string) string {
	t.Helper()
	ctx := ctxOf(t)
	var id string
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		var err error
		id, err = store.PutArtifact(ctx, tx, "controller", kind, "text/plain", []byte(body))
		return err
	}))
	return id
}

func (w *world) control(t *testing.T, id, scope, ref, kind string) {
	t.Helper()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.SetControl(ctx, tx, "owner", id, scope, ref, kind, "test") }))
}

func (w *world) clear(t *testing.T, id string) {
	t.Helper()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.ClearControl(ctx, tx, "owner", id) }))
}

func (w *world) chainHolds(t *testing.T, stream string) {
	t.Helper()
	if _, err := store.VerifyStream(ctx, w.rt, stream); err != nil {
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

func heads(m map[string]string, calls *int) store.BranchHead {
	var mu sync.Mutex
	return func(_ context.Context, _, ref string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		*calls++
		return m[ref], nil
	}
}

// The write-time fence (v5 §5.2 ③): host, epoch, live state and the
// database clock after the lock, without the lease loop (T22 ③, ⑤; T02).
func TestFenceAndLeaseClock(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.ready(t, store.Task{})
	c := w.claim(t, "h1", store.LeaseTTL, codex)
	f := fence(c, "h1")
	b, err := store.Heartbeat(ctx, w.rt, f)
	ok(t, err)
	if b.Stop || b.LeaseExpiresAt.Before(c.LeaseExpiresAt) || w.events(t, "attempt_renewed", c.AttemptID) != 0 {
		t.Fatalf("heartbeat = %+v; renewals %d", b, w.events(t, "attempt_renewed", c.AttemptID))
	}
	_, err = store.Heartbeat(ctx, w.rt, store.Fence{AttemptID: c.AttemptID, HostID: "h1", Epoch: c.Epoch + 1})
	is(t, err, store.ErrFenced)
	_, err = store.Heartbeat(ctx, w.rt, store.Fence{AttemptID: c.AttemptID, HostID: "h2", Epoch: c.Epoch})
	is(t, err, store.ErrFenced)
	is(t, store.Start(ctx, w.rt, f, "", "anthropic"), store.ErrState)
	ok(t, store.Start(ctx, w.rt, f, "gpt-x", "openai"))
	ok(t, store.Start(ctx, w.rt, f, "", ""))
	if w.state(t, "attempt", c.AttemptID) != "running" || w.state(t, "task", c.TaskID) != "running" ||
		w.events(t, "attempt_started", c.AttemptID) != 1 {
		t.Fatal("start did not move the attempt and the task once")
	}
	w.control(t, "stop-it", "task", c.TaskID, "stop")
	b, err = store.Heartbeat(ctx, w.rt, f)
	ok(t, err)
	if !b.Stop {
		t.Fatal("a stop on the task did not reach the heartbeat")
	}

	// Lapsed, within grace: renewed with an event, back to leased.
	w.ready(t, store.Task{})
	lapsed := w.claim(t, "h2", -time.Second, codex)
	b, err = store.Heartbeat(ctx, w.rt, fence(lapsed, "h2"))
	ok(t, err)
	if w.state(t, "attempt", lapsed.AttemptID) != "leased" || w.events(t, "attempt_renewed", lapsed.AttemptID) != 1 ||
		!b.LeaseExpiresAt.After(time.Now()) {
		t.Fatalf("lapsed lease: %+v", b)
	}

	// T22 ③: past the grace, the lease loop never ran: refused by the
	// database clock.
	w.ready(t, store.Task{})
	gone := w.claim(t, "h3", -store.Grace-time.Second, codex)
	_, err = store.Heartbeat(ctx, w.rt, fence(gone, "h3"))
	is(t, err, store.ErrFenced)
	is(t, store.Start(ctx, w.rt, fence(gone, "h3"), "", ""), store.ErrFenced)
	_, err = store.Checkpoint(ctx, w.rt, fence(gone, "h3"), 1, nil, false)
	is(t, err, store.ErrFenced)

	// T22 ⑤: a heartbeat that enters before the deadline but gets the row
	// only after it is refused. The positive control, the same lease with no
	// one holding the row, is renewed.
	w.ready(t, store.Task{})
	control := w.claim(t, "h4", -store.Grace+5*time.Second, codex)
	_, err = store.Heartbeat(ctx, w.rt, fence(control, "h4"))
	ok(t, err)

	w.ready(t, store.Task{})
	// Six seconds to the deadline for the setup and the wait it watches,
	// whatever the tests beside this one do (review 322 A1).
	edge := w.claim(t, "h5", -store.Grace+6*time.Second, codex)
	deadline := edge.LeaseExpiresAt.Add(store.Grace)
	pool := w.db.Pool(t, w.db.Runtime)
	holder, err := pool.Begin(ctx)
	ok(t, err)
	defer func() { _ = holder.Rollback(ctx) }()
	_, err = holder.Exec(ctx, `SELECT 1 FROM agentrt.attempt WHERE attempt_id = $1 FOR UPDATE`, edge.AttemptID)
	ok(t, err)
	if !time.Now().Before(deadline) {
		t.Fatal("the setup took longer than the lease had left; the case proves nothing")
	}
	done := make(chan error, 1)
	go func() {
		_, err := store.Heartbeat(ctx, pool, fence(edge, "h5"))
		done <- err
	}()
	w.waitFor(t, "FROM agentrt.attempt WHERE attempt_id = $1 FOR UPDATE", deadline)
	time.Sleep(time.Until(deadline) + time.Second)
	ok(t, holder.Commit(ctx))
	is(t, <-done, store.ErrFenced)
	w.chainHolds(t, "mission:M-1")
}

// T34, store half: checkpoints, computed, persist and submit, each once,
// retries answered from the key, the result checked before it is recorded.
//
// Serial: it holds the package's test hook (SetHook).
func TestResultPath(t *testing.T) {
	w := newKernelWorld(t)
	w.ready(t, store.Task{})
	c := w.claim(t, "h1", store.LeaseTTL, codex)
	f := fence(c, "h1")
	ok(t, store.Start(ctx, w.rt, f, "", ""))
	log1, log2, log2b := w.artifact(t, "log_tail", "one"), w.artifact(t, "log_tail", "two"), w.artifact(t, "log_tail", "two b")

	_, err := store.Checkpoint(ctx, w.rt, f, 1, []string{log1}, false)
	ok(t, err)
	r2, err := store.Checkpoint(ctx, w.rt, f, 2, []string{log2}, false)
	ok(t, err)
	r2again, err := store.Checkpoint(ctx, w.rt, f, 2, []string{log2}, false)
	ok(t, err)
	same(t, r2again, r2)
	_, err = store.Checkpoint(ctx, w.rt, f, 2, []string{log2b}, false)
	is(t, err, store.ErrConflict)
	_, err = store.Checkpoint(ctx, w.rt, f, 3, nil, true)
	ok(t, err)
	_, err = store.Checkpoint(ctx, w.rt, f, 4, []string{noSuch}, false)
	is(t, err, store.ErrState)
	if n := w.events(t, "attempt_checkpointed", c.AttemptID); n != 3 {
		t.Fatalf("%d checkpoint events; want 3", n)
	}

	_, err = store.Submit(ctx, w.rt, f)
	is(t, err, store.ErrState)
	exit := 0
	r, err := store.Computed(ctx, w.rt, f, &exit, "exit", "done")
	ok(t, err)
	again, err := store.Computed(ctx, w.rt, f, &exit, "exit", "done")
	ok(t, err)
	same(t, again, r)
	_, err = store.Computed(ctx, w.rt, f, &exit, "exit", "failed")
	is(t, err, store.ErrConflict)

	report := w.artifact(t, "report", "# done\n")
	other := "refs/heads/agent/" + c.TaskID + "/a9"
	calls := 0
	head := heads(map[string]string{c.PushedRef: commitA, other: commitA}, &calls)
	for _, bad := range []store.Result{
		{Report: report},
		{Report: report, Ref: other, Commit: commitA},
		{Report: report, Ref: c.PushedRef, Commit: commitB},
		{Report: noSuch, Ref: c.PushedRef, Commit: commitA},
		{Report: report, Ref: c.PushedRef},
	} {
		_, err = store.Persist(ctx, w.rt, f, bad, head)
		is(t, err, store.ErrState)
	}
	good := store.Result{Report: report, Artifacts: []string{log2}, Ref: c.PushedRef, Commit: commitA}
	p, err := store.Persist(ctx, w.rt, f, good, head)
	ok(t, err)
	before := calls
	p2, err := store.Persist(ctx, w.rt, f, good, head)
	ok(t, err)
	same(t, p2, p)
	if calls != before || w.events(t, "attempt_persisted", c.AttemptID) != 1 {
		t.Fatalf("the retry read Gitea %d times; %d persisted events", calls-before, w.events(t, "attempt_persisted", c.AttemptID))
	}

	// The submit's answer is lost and three retries race. A fourth
	// connection holds the key's lock while all three miss the key, and all
	// three must then wait for that lock (review 149 B3).
	pool := w.db.Pool(t, w.db.Runtime)
	holder, err := pool.Begin(ctx)
	ok(t, err)
	defer func() { _ = holder.Rollback(ctx) }()
	_, err = holder.Exec(ctx, `SELECT pg_advisory_xact_lock(742103, hashtext($1))`, "host:h1|submit|"+c.AttemptID)
	ok(t, err)
	answers := make([]map[string]any, 3)
	errs := make([]error, 3)
	var missed sync.WaitGroup
	missed.Add(len(answers))
	unhook := store.SetHook(func(point, _ string) {
		if point == "missed" {
			missed.Done()
		}
	})
	var wg sync.WaitGroup
	for i := range answers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			answers[i], errs[i] = store.Submit(ctx, pool, f)
		}(i)
	}
	missed.Wait()
	w.waitForN(t, "pg_advisory_xact_lock(742103", len(answers), time.Now().Add(10*time.Second))
	ok(t, holder.Commit(ctx))
	wg.Wait()
	unhook()
	for i := range answers {
		ok(t, errs[i])
		same(t, answers[i], answers[0])
	}
	if w.events(t, "attempt_submitted", c.AttemptID) != 1 || w.state(t, "task", c.TaskID) != "submitted" ||
		w.state(t, "attempt", c.AttemptID) != "submitted" {
		t.Fatal("submit did not happen exactly once")
	}
	// B1: the attempt is no longer live and a retry still gets the answer;
	// anything new is fenced.
	late, err := store.Submit(ctx, w.rt, f)
	ok(t, err)
	same(t, late, answers[0])
	_, err = store.Heartbeat(ctx, w.rt, f)
	is(t, err, store.ErrFenced)
	_, err = store.Checkpoint(ctx, w.rt, f, 9, nil, false)
	is(t, err, store.ErrFenced)
	_, err = store.Salvage(ctx, w.rt, c.AttemptID, "h1", store.Result{}, head)
	is(t, err, store.ErrState)
	w.chainHolds(t, "mission:M-1")
}

// T25, store half for the kernel: no function moves a task or a mission
// outside its transition table, and each move is one event.
func TestOnlyListedMoves(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	task := w.ready(t, store.Task{})
	for _, to := range []string{"leased", "running", "submitted", "accepted", "ready"} {
		err := store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveTask(ctx, tx, "controller", task, to, "") })
		is(t, err, store.ErrState)
	}
	c := w.claim(t, "h1", store.LeaseTTL, codex)
	for _, to := range []string{"ready", "cancelled", "blocked"} {
		err := store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveTask(ctx, tx, "controller", c.TaskID, to, "") })
		is(t, err, store.ErrState)
	}
	err := store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-1", "proposed", "") })
	is(t, err, store.ErrState)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-2", "active", "") }))

	is(t, store.RegisterHost(ctx, w.rt, "owner", "h9", "laptop"), store.ErrState)
	ok(t, store.RegisterHost(ctx, w.rt, "owner", "h1", "dev"))
	is(t, store.RegisterHost(ctx, w.rt, "owner", "h1", "vm"), store.ErrState)

	var moves, changes int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'task_state_changed' AND task_id = $1`, task).Scan(&moves))
	ok(t, w.rt.QueryRow(ctx, `SELECT version FROM agentrt.task WHERE task_id = $1`, task).Scan(&changes))
	if moves != 2 || changes != 2 {
		t.Fatalf("%d move events, version %d; want 2 and 2 (ready, leased)", moves, changes)
	}
	var reasons []string
	rows, err := w.rt.Query(ctx, `SELECT body_json->>'to' FROM agentrt.event WHERE event_type = 'task_state_changed' AND task_id = $1 ORDER BY event_id`, task)
	ok(t, err)
	reasons, err = pgx.CollectRows(rows, pgx.RowTo[string])
	ok(t, err)
	if strings.Join(reasons, ",") != "ready,leased" {
		t.Fatalf("moves = %v", reasons)
	}
	w.chainHolds(t, "mission:M-1")
	w.chainHolds(t, "host:h1")
}
