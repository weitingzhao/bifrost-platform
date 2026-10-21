package store_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// keptAttempt is an attempt as Progress says what of it is persisted.
type keptAttempt struct {
	AttemptID         string `json:"attempt_id"`
	AttemptNo         int    `json:"attempt_no"`
	HostID            string `json:"host_id"`
	State             string `json:"state"`
	Status            string `json:"status"`
	Reason            string `json:"reason"`
	Persisted         bool   `json:"persisted"`
	LastCheckpointSeq *int64 `json:"last_checkpoint_seq"`
	LastCheckpointAt  string `json:"last_checkpoint_at"`
	UnpersistedForS   *int64 `json:"unpersisted_for_s"`
	EndedAt           string `json:"ended_at"`
}

// keptDoc is the part of Progress that says it: the live attempts and the
// tasks with their attempt and their lost one.
type keptDoc struct {
	Live []keptAttempt `json:"live"`
	Work struct {
		Missions []struct {
			Tasks []struct {
				TaskID      string       `json:"task_id"`
				Status      string       `json:"status"`
				Reason      string       `json:"reason"`
				Attempt     *keptAttempt `json:"attempt"`
				LostAttempt *keptAttempt `json:"lost_attempt"`
			} `json:"tasks"`
		} `json:"missions"`
	} `json:"work"`
}

// The Console's fixture of these moments (console/src/api/__fixtures__/agentrt-progress-persist.testdb.json):
// the projection from these rows when the attempt is stale and when it is lost, written when
// CONSOLE_PERSIST_FIXTURE_WRITE=1.
const consolePersistFixture = "../../../../console/src/api/__fixtures__/agentrt-progress-persist.testdb.json"

func (w *world) kept(t *testing.T) keptDoc {
	t.Helper()
	doc, _ := w.keptRaw(t)
	return doc
}

func (w *world) keptRaw(t *testing.T) (keptDoc, json.RawMessage) {
	t.Helper()
	out, err := store.Progress(ctx, w.rt)
	ok(t, err)
	raw, err := json.Marshal(out)
	ok(t, err)
	var doc keptDoc
	ok(t, json.Unmarshal(raw, &doc))
	return doc, raw
}

// task is the task's row in the doc and the live attempt the doc lists for
// it, if any.
func (d keptDoc) task(t *testing.T, id string) (status, reason string, attempt, lost *keptAttempt, live *keptAttempt) {
	t.Helper()
	for _, m := range d.Work.Missions {
		for _, x := range m.Tasks {
			if x.TaskID == id {
				status, reason, attempt, lost = x.Status, x.Reason, x.Attempt, x.LostAttempt
			}
		}
	}
	if status == "" {
		t.Fatalf("task %s is not in the projection", id)
	}
	for i, a := range d.Live {
		if attempt != nil && a.AttemptID == attempt.AttemptID {
			live = &d.Live[i]
		}
	}
	return status, reason, attempt, lost, live
}

// TD-352 (design v5 §5.2 T34 ⑤; Gate A STG 2026-10-15 M-5: gate-b
// checkpointed once and went away for good, and Progress said only
// maybe_stuck). An attempt whose lease lapsed after checkpoint 1 says its
// result is not persisted, checkpoint 1 and when, and how long nothing was
// persisted, in its fields and its reason, and so does its task; on a host
// that stopped polling too. Once lost, the task, back in the queue, carries
// that attempt and says it was lost with its result not persisted; never
// done. Salvaged, the lost attempt says its result was persisted after. An
// attempt that persisted its result before its lease lapsed says nothing of
// the kind, and a working attempt with no checkpoint only counts from its
// start.
func TestProgressSaysWhatOfAnAttemptIsPersisted(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	// Results with no branch (effect none), so persisting one needs no push.
	claimed := func(host string) (string, store.Fence, string) {
		t.Helper()
		id := w.ready(t, store.Task{Effect: "none"})
		c := w.claim(t, host, time.Hour, codex)
		if c == nil || c.TaskID != id {
			t.Fatalf("claim on %s: %+v; want %s", host, c, id)
		}
		f := fence(c, host)
		ok(t, store.Start(ctx, w.rt, f, "", ""))
		return id, f, c.AttemptID
	}
	lapse := func(attempt, by string) {
		t.Helper()
		// A lease moves back only with another column and the import event
		// (liveness columns alone move forward only).
		w.legacy(t, `UPDATE agentrt.attempt SET lease_expires_at = clock_timestamp() - interval '`+by+`',
		  started_at = started_at - interval '1 second' WHERE attempt_id = '`+attempt+`'`)
		for range 2 {
			_, _, err := store.SweepLeases(ctx, w.rt)
			ok(t, err)
		}
	}

	gone, f, goneAttempt := claimed("h2")
	_, err := store.Checkpoint(ctx, w.rt, f, 1, nil, false)
	ok(t, err)
	kept, kf, keptAttemptID := claimed("h4")
	exit := 0
	_, err = store.Computed(ctx, w.rt, kf, &exit, "exit", "done")
	ok(t, err)
	_, err = store.Persist(ctx, w.rt, kf, store.Result{Report: w.artifact(t, "report", "done")}, heads(nil, new(int)))
	ok(t, err)
	working, _, _ := claimed("h1")
	lapse(goneAttempt, "1 second")
	lapse(keptAttemptID, "1 second")
	if w.state(t, "attempt", goneAttempt) != "stale" || w.state(t, "attempt", keptAttemptID) != "stale" {
		t.Fatalf("the lapsed attempts are %s and %s; want stale", w.state(t, "attempt", goneAttempt), w.state(t, "attempt", keptAttemptID))
	}

	doc, staleRaw := w.keptRaw(t)
	status, reason, a, _, live := doc.task(t, gone)
	want := "the lease lapsed and was not renewed; result not persisted (last checkpoint 1 at "
	if status != store.StatusMaybeStuck || !strings.HasPrefix(reason, want) || !strings.Contains(reason, ", nothing persisted for ") ||
		a == nil || a.Persisted || a.LastCheckpointSeq == nil || *a.LastCheckpointSeq != 1 || a.LastCheckpointAt == "" ||
		a.UnpersistedForS == nil || *a.UnpersistedForS < 0 || live == nil || live.Reason != reason || live.LastCheckpointAt != a.LastCheckpointAt {
		t.Fatalf("the stale attempt with checkpoint 1: %s %q, attempt %+v, live %+v", status, reason, a, live)
	}
	if _, perr := time.Parse(time.RFC3339Nano, a.LastCheckpointAt); perr != nil {
		t.Fatalf("its checkpoint's time %q: %v", a.LastCheckpointAt, perr)
	}
	status, reason, a, _, _ = doc.task(t, kept)
	if status != store.StatusMaybeStuck || reason != "the lease lapsed and was not renewed" || a == nil || !a.Persisted || a.UnpersistedForS != nil {
		t.Fatalf("the stale attempt that persisted its result: %s %q, %+v", status, reason, a)
	}
	status, reason, a, _, _ = doc.task(t, working)
	if status != store.StatusWorking || strings.Contains(reason, "persisted") || a == nil || a.Persisted || a.LastCheckpointSeq != nil ||
		a.UnpersistedForS == nil {
		t.Fatalf("the working attempt: %s %q, %+v", status, reason, a)
	}

	// Its host stops polling: lost host, the same words.
	w.legacy(t, `UPDATE agentrt.host SET last_poll_at = clock_timestamp() - interval '10 minutes' WHERE host_id = 'h2'`)
	status, reason, _, _, _ = w.kept(t).task(t, gone)
	if status != store.StatusHostLost || !strings.Contains(reason, "; result not persisted (last checkpoint 1 at ") {
		t.Fatalf("on a host that stopped polling: %s %q", status, reason)
	}

	// Past the grace: lost, the task back in the queue.
	lapse(goneAttempt, "1 hour")
	if w.state(t, "attempt", goneAttempt) != "lost" || w.state(t, "task", gone) != "ready" {
		t.Fatalf("past the grace: attempt %s, task %s", w.state(t, "attempt", goneAttempt), w.state(t, "task", gone))
	}
	lostDoc, lostRaw := w.keptRaw(t)
	status, reason, a, lost, _ := lostDoc.task(t, gone)
	if a != nil || lost == nil || lost.AttemptID != goneAttempt || lost.State != "lost" || lost.Status != "" || lost.Persisted ||
		lost.LastCheckpointSeq == nil || *lost.LastCheckpointSeq != 1 || lost.EndedAt == "" ||
		!strings.HasPrefix(lost.Reason, "attempt 1 on h2 was lost at "+lost.EndedAt+"; result not persisted (last checkpoint 1 at ") {
		t.Fatalf("the lost attempt: live %+v, lost %+v", a, lost)
	}
	if status != store.StatusQueued || reason != "ready for a host to claim; "+lost.Reason {
		t.Fatalf("the task with a lost attempt: %s %q", status, reason)
	}
	if os.Getenv("CONSOLE_PERSIST_FIXTURE_WRITE") == "1" {
		raw, err := json.MarshalIndent(map[string]any{"gone_task": gone, "gone_attempt": goneAttempt, "stale": staleRaw, "lost": lostRaw}, "", "  ")
		ok(t, err)
		ok(t, os.WriteFile(consolePersistFixture, append(raw, '\n'), 0o644))
	}

	// Salvaged: its result was persisted after.
	_, err = store.Salvage(ctx, w.rt, goneAttempt, "h2", store.Result{Report: w.artifact(t, "report", "salvaged")}, heads(nil, new(int)))
	ok(t, err)
	status, reason, _, lost, _ = w.kept(t).task(t, gone)
	if lost == nil || !lost.Persisted || lost.UnpersistedForS != nil || !strings.HasSuffix(lost.Reason, "; its result was persisted after (salvaged)") ||
		status != store.StatusQueued || reason != "ready for a host to claim" {
		t.Fatalf("salvaged: %s %q, lost %+v", status, reason, lost)
	}

	// A later attempt persists its result: the lost one is no longer the task's news.
	c := w.claim(t, "h3", time.Hour, codex)
	if c == nil || c.TaskID != gone {
		t.Fatalf("claim on h3: %+v; want %s", c, gone)
	}
	f3 := fence(c, "h3")
	ok(t, store.Start(ctx, w.rt, f3, "", ""))
	_, err = store.Computed(ctx, w.rt, f3, &exit, "exit", "done")
	ok(t, err)
	_, err = store.Persist(ctx, w.rt, f3, store.Result{Report: w.artifact(t, "report", "again")}, heads(nil, new(int)))
	ok(t, err)
	if _, _, a, lost, _ = w.kept(t).task(t, gone); a == nil || a.AttemptNo != 2 || !a.Persisted || lost != nil {
		t.Fatalf("after attempt 2 persisted: attempt %+v, lost %+v", a, lost)
	}
}
