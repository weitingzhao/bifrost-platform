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
	next := w.claim(t, "h2", store.LeaseTTL, cursor, codex)
	if next == nil || next.TaskID != task || next.Tool.Executor != codex.Executor {
		t.Fatalf("the retry did not go to codex at once: %+v", next)
	}
	w.fail(t, next, "h2")
	// Each executor has failed once; cursor's 30 s run from its own failure.
	w.ago(t, task, 31*time.Second)
	back := w.claim(t, "h1", store.LeaseTTL, cursor)
	if back == nil || back.TaskID != task {
		t.Fatalf("cursor did not take the task back after its wait: %+v", back)
	}
}

// TD-381: an attempt that ended for a reason outside its executor (a restart,
// a stop, a lost lease) does not count, and it ends the run of failures.
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
	w.ago(t, task, 31*time.Second)
	third := w.claim(t, "h1", store.LeaseTTL, cursor)
	if third == nil {
		t.Fatal("not leased again after one failure and 31 s")
	}
	ok(t, store.Start(ctx, w.rt, fence(third, "h1"), "", ""))
	_, err = store.Ended(ctx, w.rt, third.AttemptID, "h1", "cancelled", true)
	ok(t, err)
	if c := w.claim(t, "h1", store.LeaseTTL, cursor); c == nil {
		t.Fatal("a cancelled attempt after a failure still held the executor back")
	}
}

// TD-382: a report agentd uploads and the platform refuses is recorded on the
// attempt handing over on that host, with the rule; the attempt's end says
// so; nothing is recorded when the host has no attempt handing over, or for
// a kind that is not a report.
func TestRefusedReportIsRecorded(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	task := w.ready(t, store.Task{})
	c := w.claim(t, "h1", store.LeaseTTL, cursor)
	ok(t, store.Start(ctx, w.rt, fence(c, "h1"), "", ""))
	refused := "sha256:" + strings.Repeat("ab", 32)

	// Not yet computed: the upload is not the hand-over.
	on, err := store.RefusedReport(ctx, w.rt, "h1", "review", refused, "local_path")
	ok(t, err)
	if on != "" {
		t.Fatalf("a refusal before the attempt computed was recorded on %s", on)
	}
	exit := 0
	_, err = store.Computed(ctx, w.rt, fence(c, "h1"), &exit, "exit", "done")
	ok(t, err)
	for _, cs := range []struct{ host, kind, rule string }{
		{"h2", "review", "local_path"},     // another host
		{"h1", "log_tail", "local_path"},   // not a report
		{"h1", "review", "something_else"}, // not a refusal rule
	} {
		on, err = store.RefusedReport(ctx, w.rt, cs.host, cs.kind, refused, cs.rule)
		ok(t, err)
		if on != "" {
			t.Fatalf("%v was recorded on %s", cs, on)
		}
	}
	for range 2 {
		on, err = store.RefusedReport(ctx, w.rt, "h1", "review", refused, "local_path")
		ok(t, err)
		if on != c.AttemptID {
			t.Fatalf("the refusal was recorded on %q; want %s", on, c.AttemptID)
		}
	}
	if n := w.alertsOf(t, task, "local_path"); n != 1 {
		t.Fatalf("%d local_path alerts for one refused body; want 1", n)
	}
	var source, phrase, artifact string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'source', body_json->>'phrase', body_json->>'artifact_id' FROM agentrt.event
	  WHERE attempt_id = $1 AND event_type = 'policy_alert'`, c.AttemptID).Scan(&source, &phrase, &artifact))
	if source != "review" || phrase != "a local path (H8)" || artifact != refused {
		t.Fatalf("alert body: %s / %s / %s", source, phrase, artifact)
	}
	_, err = store.Persist(ctx, w.rt, fence(c, "h1"), store.Result{}, nil)
	is(t, err, store.ErrState)
	_, err = store.Ended(ctx, w.rt, c.AttemptID, "h1", "exit", true)
	ok(t, err)
	var reason string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'reason' FROM agentrt.event WHERE task_id = $1 AND event_type = 'task_state_changed'
	  AND body_json->>'from' = 'running' AND body_json->>'to' = 'ready'`, task).Scan(&reason))
	if !strings.Contains(reason, "its report was refused by rule local_path") {
		t.Fatalf("the task's move says %q", reason)
	}
	if next := w.claim(t, "h1", store.LeaseTTL, cursor); next != nil {
		t.Fatalf("the executor whose report was refused was leased the task again at once: %+v", next)
	}

	// An attempt that computed and ends without a refusal on record still
	// says its result did not reach the platform.
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
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'reason' FROM agentrt.event WHERE attempt_id IS NULL AND task_id = $1
	  AND event_type = 'task_state_changed' ORDER BY event_id DESC LIMIT 1`, task).Scan(&reason))
	if !strings.Contains(reason, "computed but not persisted") {
		t.Fatalf("the second move says %q", reason)
	}
	w.chainHolds(t, "mission:M-1")
}
