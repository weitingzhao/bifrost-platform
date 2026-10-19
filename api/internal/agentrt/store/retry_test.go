package store_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// fail runs a leased attempt to a failure of its executor's doing: started,
// then ended by its exit with no result handed over.
func (w *world) fail(t *testing.T, c *store.Claim, host string) {
	t.Helper()
	ok(t, store.Start(ctx, w.rt, fence(c, host), "", ""))
	r, err := store.Ended(ctx, w.rt, c.AttemptID, host, "exit", true)
	ok(t, err)
	if r["state"] != "failed" {
		t.Fatalf("ended = %v", r)
	}
}

// ago moves the end of every failed attempt of task d into the past, as if
// that much time had gone by.
func (w *world) ago(t *testing.T, task string, d time.Duration) {
	t.Helper()
	w.legacy(t, fmt.Sprintf(`UPDATE agentrt.attempt SET ended_at = ended_at - make_interval(secs => %f) WHERE task_id = '%s'
	  AND state = 'failed'`, d.Seconds(), task))
}

func (w *world) alertsOf(t *testing.T, task, rule string) int {
	t.Helper()
	var n int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE task_id = $1 AND event_type = 'policy_alert'
	  AND body_json->>'rule' = $2`, task, rule).Scan(&n))
	return n
}

// TD-381: an executor that fails a task within seconds is not leased it again
// at once. Three failures in a row lease the task at most twice in the first
// minute (30 s after the first, 60 s after the second), and the third raises
// a policy_alert and keeps that executor from the task; another candidate
// may still take it at once.
func TestFailingExecutorWaitsAndStops(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	task := w.ready(t, store.Task{})

	first := w.claim(t, "h1", store.LeaseTTL, cursor)
	if first == nil || first.TaskID != task {
		t.Fatalf("first claim = %+v", first)
	}
	w.fail(t, first, "h1")
	if c := w.claim(t, "h1", store.LeaseTTL, cursor); c != nil {
		t.Fatalf("leased again at once after a failure: %+v", c)
	}
	w.ago(t, task, 29*time.Second)
	if c := w.claim(t, "h1", store.LeaseTTL, cursor); c != nil {
		t.Fatalf("leased again 29 s after a failure: %+v", c)
	}
	w.ago(t, task, 2*time.Second)
	second := w.claim(t, "h1", store.LeaseTTL, cursor)
	if second == nil || second.TaskID != task {
		t.Fatalf("not leased again 31 s after the first failure: %+v", second)
	}
	w.fail(t, second, "h1")
	w.ago(t, task, 31*time.Second)
	if c := w.claim(t, "h1", store.LeaseTTL, cursor); c != nil {
		t.Fatalf("leased again 31 s after a second failure in a row (wants 60 s): %+v", c)
	}
	if n := w.alertsOf(t, task, "repeated_failure"); n != 0 {
		t.Fatalf("%d alerts after two failures", n)
	}
	w.ago(t, task, 30*time.Second)
	third := w.claim(t, "h1", store.LeaseTTL, cursor)
	if third == nil || third.TaskID != task {
		t.Fatalf("not leased again 61 s after the second failure: %+v", third)
	}
	w.fail(t, third, "h1")
	if n := w.alertsOf(t, task, "repeated_failure"); n != 1 {
		t.Fatalf("%d repeated_failure alerts after the third failure; want 1", n)
	}
	var on, executor string
	ok(t, w.rt.QueryRow(ctx, `SELECT e.attempt_id, a.executor_actor_id FROM agentrt.event e JOIN agentrt.attempt a USING (attempt_id)
	  WHERE e.task_id = $1 AND e.event_type = 'policy_alert'`, task).Scan(&on, &executor))
	if on != third.AttemptID || executor != cursor.Executor {
		t.Fatalf("the alert is on %s by %s; want the third attempt %s", on, executor, third.AttemptID)
	}
	w.ago(t, task, time.Hour)
	if c := w.claim(t, "h1", store.LeaseTTL, cursor); c != nil {
		t.Fatalf("an executor that failed three times in a row was leased the task again: %+v", c)
	}
	other := w.claim(t, "h2", store.LeaseTTL, codex)
	if other == nil || other.TaskID != task {
		t.Fatalf("another candidate did not take the task: %+v", other)
	}
	if n := w.alertsOf(t, task, "repeated_failure"); n != 1 {
		t.Fatalf("%d repeated_failure alerts; the alert is recorded once", n)
	}
	w.chainHolds(t, "mission:M-1")
}

// TD-381: the retry goes to another candidate at once (the cascade), and the
// executor that failed takes the task back once its wait is over.
func TestRetryGoesToAnotherCandidate(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	task := w.ready(t, store.Task{})
	c := w.claim(t, "h1", store.LeaseTTL, cursor)
	w.fail(t, c, "h1")
	if held := w.claim(t, "h2", store.LeaseTTL, cursor); held != nil {
		t.Fatalf("cursor was offered the task at once after its failure: %+v", held)
	}
	next := w.claim(t, "h2", store.LeaseTTL, cursor, codex)
	if next == nil || next.TaskID != task || next.Tool.Executor != codex.Executor {
		t.Fatalf("the retry did not go to codex at once: %+v", next)
	}
	w.fail(t, next, "h2")
	// Each executor has failed once; cursor's 30 s run from its own failure.
	if held := w.claim(t, "h1", store.LeaseTTL, cursor, codex); held != nil {
		t.Fatalf("an executor was offered the task at once after its failure: %+v", held)
	}
	w.ago(t, task, 31*time.Second)
	back := w.claim(t, "h1", store.LeaseTTL, cursor)
	if back == nil || back.TaskID != task {
		t.Fatalf("cursor did not take the task back after its wait: %+v", back)
	}
}

// TD-381: an attempt that ended for a reason outside its executor (a restart,
// a stop, a lost lease) does not count, and it ends the run of failures: the
// next failures count from one again.
func TestOnlyTheExecutorsFailuresCount(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	task := w.ready(t, store.Task{})
	c := w.claim(t, "h1", store.LeaseTTL, cursor)
	ok(t, store.Start(ctx, w.rt, fence(c, "h1"), "", ""))
	_, err := store.Ended(ctx, w.rt, c.AttemptID, "h1", "daemon_restart", true)
	ok(t, err)
	again := w.claim(t, "h1", store.LeaseTTL, cursor)
	if again == nil || again.TaskID != task {
		t.Fatalf("a restart held the executor back: %+v", again)
	}
	w.fail(t, again, "h1")
	if held := w.claim(t, "h1", store.LeaseTTL, cursor); held != nil {
		t.Fatalf("leased again at once after a failure: %+v", held)
	}
	w.ago(t, task, 31*time.Second)
	third := w.claim(t, "h1", store.LeaseTTL, cursor)
	if third == nil {
		t.Fatal("not leased again after one failure and 31 s")
	}
	ok(t, store.Start(ctx, w.rt, fence(third, "h1"), "", ""))
	_, err = store.Ended(ctx, w.rt, third.AttemptID, "h1", "cancelled", true)
	ok(t, err)
	// The cancelled attempt ended the run: two failures now are a first and
	// a second, waiting 30 s and then 60 s, and raise no alert.
	fourth := w.claim(t, "h1", store.LeaseTTL, cursor)
	if fourth == nil {
		t.Fatal("a cancelled attempt after a failure still held the executor back")
	}
	w.fail(t, fourth, "h1")
	w.ago(t, task, 31*time.Second)
	fifth := w.claim(t, "h1", store.LeaseTTL, cursor)
	if fifth == nil {
		t.Fatal("the first failure of a new run waited more than 30 s")
	}
	w.fail(t, fifth, "h1")
	w.ago(t, task, 31*time.Second)
	if held := w.claim(t, "h1", store.LeaseTTL, cursor); held != nil {
		t.Fatalf("the second failure of a new run waited only 30 s: %+v", held)
	}
	if n := w.alertsOf(t, task, "repeated_failure"); n != 0 {
		t.Fatalf("%d repeated_failure alerts for runs of one and two failures", n)
	}
}

func (w *world) hostAlerts(t *testing.T, host string) []map[string]any {
	t.Helper()
	rows, err := w.rt.Query(ctx, `SELECT body_json, attempt_id IS NULL FROM agentrt.event WHERE event_stream_id = $1
	  AND event_type = 'policy_alert' ORDER BY event_id`, "host:"+host)
	ok(t, err)
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var b map[string]any
		var noAttempt bool
		ok(t, rows.Scan(&b, &noAttempt))
		if !noAttempt {
			t.Fatalf("a refusal names an attempt: %v", b)
		}
		out = append(out, b)
	}
	ok(t, rows.Err())
	return out
}

func (w *world) lastReason(t *testing.T, task string) string {
	t.Helper()
	var reason string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'reason' FROM agentrt.event WHERE task_id = $1 AND event_type = 'task_state_changed'
	  AND body_json->>'to' = 'ready' ORDER BY event_id DESC LIMIT 1`, task).Scan(&reason))
	return reason
}

// TD-382: a report agentd uploads and the platform refuses is recorded on
// its host's stream with the rule, once per body; nothing for a kind that is
// not a report or a rule that is not a refusal. An attempt of the host that
// ends without its result says so, and names a refusal of the host since it
// computed as what happened meanwhile, never as its own (review 400 A1: an
// upload does not name its attempt).
func TestRefusedReportIsRecorded(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	task := w.ready(t, store.Task{})
	c := w.claim(t, "h1", store.LeaseTTL, cursor)
	ok(t, store.Start(ctx, w.rt, fence(c, "h1"), "", ""))
	early := "sha256:" + strings.Repeat("ab", 32)
	late := "sha256:" + strings.Repeat("cd", 32)

	for _, cs := range []struct{ host, kind, rule string }{
		{"h1", "log_tail", "local_path"},   // not a report
		{"h1", "review", "something_else"}, // not a refusal rule
		{"", "review", "local_path"},       // no host
	} {
		on, err := store.RefusedReport(ctx, w.rt, cs.host, cs.kind, early, cs.rule)
		ok(t, err)
		if on {
			t.Fatalf("%v was recorded", cs)
		}
	}
	for range 2 {
		on, err := store.RefusedReport(ctx, w.rt, "h1", "review", early, "local_path")
		ok(t, err)
		if !on {
			t.Fatal("the refusal is not on record")
		}
	}
	got := w.hostAlerts(t, "h1")
	if len(got) != 1 || got[0]["rule"] != "local_path" || got[0]["source"] != "review" || got[0]["phrase"] != "a local path (H8)" ||
		got[0]["artifact_id"] != early {
		t.Fatalf("host alerts after one body refused twice: %v", got)
	}
	exit := 0
	_, err := store.Computed(ctx, w.rt, fence(c, "h1"), &exit, "exit", "done")
	ok(t, err)
	_, err = store.RefusedReport(ctx, w.rt, "h1", "report", late, "workspace_ref")
	ok(t, err)
	_, err = store.Persist(ctx, w.rt, fence(c, "h1"), store.Result{}, nil)
	is(t, err, store.ErrState)
	_, err = store.Ended(ctx, w.rt, c.AttemptID, "h1", "exit", true)
	ok(t, err)
	// The refusal before it computed is not named; the one since is, as the
	// host's.
	if r := w.lastReason(t, task); !strings.Contains(r, "its result was computed but not persisted; meanwhile its host had a report refused by rule workspace_ref") {
		t.Fatalf("the task's move says %q", r)
	}
	if next := w.claim(t, "h1", store.LeaseTTL, cursor); next != nil {
		t.Fatalf("the executor whose result did not land was leased the task again at once: %+v", next)
	}

	// An attempt that computes and ends with no refusal of its host since
	// says only that its result did not land.
	w.ago(t, task, time.Minute)
	d := w.claim(t, "h1", store.LeaseTTL, cursor)
	if d == nil {
		t.Fatal("not leased again after the wait")
	}
	ok(t, store.Start(ctx, w.rt, fence(d, "h1"), "", ""))
	_, err = store.Computed(ctx, w.rt, fence(d, "h1"), &exit, "exit", "done")
	ok(t, err)
	_, err = store.Ended(ctx, w.rt, d.AttemptID, "h1", "exit", true)
	ok(t, err)
	if r := w.lastReason(t, task); !strings.HasSuffix(r, "its result was computed but not persisted") {
		t.Fatalf("the second move says %q", r)
	}
	w.chainHolds(t, "mission:M-1")
	w.chainHolds(t, "host:h1")
}

// Review 400 A1: a salvage upload of a lost attempt's report is refused while
// another attempt of the same host hands over. The refusal is the host's: it
// is named on no attempt, and the other attempt's end tells it as what
// happened on its host meanwhile.
func TestARefusedSalvageIsNoOtherAttemptsCause(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	lostTask := w.ready(t, store.Task{})
	lost := w.claim(t, "h3", -store.Grace-time.Second, codex)
	if lost == nil || lost.TaskID != lostTask {
		t.Fatalf("claim = %+v", lost)
	}
	_, _, err := store.SweepLeases(ctx, w.rt)
	ok(t, err)
	if w.state(t, "attempt", lost.AttemptID) != "lost" {
		t.Fatal("the attempt was not lost")
	}
	// The next attempt of the host, of that task or another.
	w.ready(t, store.Task{})
	b := w.claim(t, "h3", store.LeaseTTL, codex)
	if b == nil || b.AttemptID == lost.AttemptID {
		t.Fatalf("second claim = %+v", b)
	}
	ok(t, store.Start(ctx, w.rt, fence(b, "h3"), "", ""))
	exit := 0
	_, err = store.Computed(ctx, w.rt, fence(b, "h3"), &exit, "exit", "done")
	ok(t, err)
	_, err = store.RefusedReport(ctx, w.rt, "h3", "report", "sha256:"+strings.Repeat("ef", 32), "local_path")
	ok(t, err)
	if n := len(w.hostAlerts(t, "h3")); n != 1 {
		t.Fatalf("%d refusals on the host", n)
	}
	var onAttempts int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'policy_alert' AND attempt_id IS NOT NULL`).Scan(&onAttempts))
	if onAttempts != 0 {
		t.Fatalf("%d refusals were put on an attempt", onAttempts)
	}
	_, err = store.Ended(ctx, w.rt, b.AttemptID, "h3", "exit", true)
	ok(t, err)
	if r := w.lastReason(t, b.TaskID); !strings.Contains(r, "computed but not persisted; meanwhile its host had a report refused by rule local_path") ||
		strings.Contains(r, "its report was refused") {
		t.Fatalf("the other attempt's end says %q", r)
	}
}

// Review 401 B1: a run longer than a page (failures from before the stop
// existed) is read whole: an executor whose three failures sit under 64
// newer ones of another is still passed by, and a break inside the first
// page ends the run there.
func TestALongRunIsReadWhole(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	task := w.ready(t, store.Task{})
	insert := func(from, to int, executor, state string) string {
		return fmt.Sprintf(`INSERT INTO agentrt.attempt (attempt_id, task_id, attempt_no, executor_actor_id, host_id, model_provider,
		  lease_epoch, lease_expires_at, heartbeat_at, state, started_at, ended_at, end_how)
		  SELECT 'a_' || lpad(n::text, 26, '0'), '%s', n, '%s', 'h1', 'x', nextval('agentrt.lease_epoch_seq'),
		    clock_timestamp() - interval '2 hours', clock_timestamp() - interval '2 hours', '%s',
		    clock_timestamp() - interval '2 hours', clock_timestamp() - interval '1 hour', 'exit'
		  FROM generate_series(%d, %d) n`, task, executor, state, from, to)
	}
	w.legacy(t, insert(1, 3, cursor.Executor, "failed"), insert(4, 67, codex.Executor, "failed"))
	if c := w.claim(t, "h1", store.LeaseTTL, cursor); c != nil {
		t.Fatalf("an executor whose three failures lie under 64 newer ones was leased the task: %+v", c)
	}
	if c := w.claim(t, "h2", store.LeaseTTL, codex); c != nil {
		t.Fatalf("codex, 64 failures in a row, was leased the task: %+v", c)
	}
	// A break inside the first page: an attempt taken (finished) after the
	// 67 ends the run, so neither executor is held back by what came before.
	w.legacy(t, insert(68, 68, codex.Executor, "finished"))
	if c := w.claim(t, "h1", store.LeaseTTL, cursor); c == nil {
		t.Fatal("a run ended by a finished attempt still held cursor back")
	}
}
