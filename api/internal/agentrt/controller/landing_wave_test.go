package controller_test

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// openWave opens a bifrost-platform wave on the rig's main.
func (r *batchRig) openWave(t *testing.T) store.Wave {
	t.Helper()
	w, err := store.OpenWave(ctx, r.pool, store.Writer{Actor: "thread:s1", Token: "operator"}, "bifrost-platform", "R6",
		func(context.Context, string, string) (string, error) { return batchM0, nil })
	must(t, err)
	return w
}

// reaccept accepts task's next round: a new attempt whose commit changes
// path, as an executor's rework after a red landing would.
func (r *batchRig) reaccept(t *testing.T, task, path string, round int) {
	t.Helper()
	sum := sha1.Sum([]byte(task + "/" + path))
	a := hex.EncodeToString(sum[:])
	r.mu.Lock()
	r.paths[a] = path
	r.mu.Unlock()
	attempt := store.NewAttemptID()
	op := r.state(t).LatestOperation(task)
	must(t, store.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		var from string
		if err := tx.QueryRow(ctx, `SELECT state FROM agentrt.task WHERE task_id = $1`, task).Scan(&from); err != nil {
			return err
		}
		evidence, err := store.PutArtifact(ctx, tx, "controller", "report", "text/plain", []byte("pass again "+path+"\n"))
		if err != nil {
			return err
		}
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller", Body: map[string]any{"source": "test"}}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.attempt (attempt_id, task_id, attempt_no, executor_actor_id, lease_epoch,
		    lease_expires_at, heartbeat_at, state, conclusion, pushed_ref, pushed_commit, computed_at, persisted_at, report_artifact_id)
		  VALUES ($1, $2, $3, 'controller', 1, now(), now(), 'finished', 'done', $4, $5, now(), now(), $6)`,
			attempt, task, round, store.AttemptRef(task, round), a, evidence); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.verification (verification_id, task_id, attempt_id, method, verdict,
		    evidence_artifact_id, subject_commit, accepts_attempt) VALUES ($1, $2, $3, 'landing_ci', 'pass', $4, $5, true)`,
			store.NewVerificationID(), task, attempt, evidence, a); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE agentrt.task SET state = 'accepted' WHERE task_id = $1`, task); err != nil {
			return err
		}
		if _, err = store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-3"), Type: "task_state_changed", Actor: "controller",
			MissionID: "M-3", TaskID: task, Body: map[string]any{"from": from, "to": "accepted", "reason": "accepted on submission"}}); err != nil {
			return err
		}
		// As accepting a reworked attempt does (store retryReworkedLanding):
		// its red landing is retried.
		_, err = store.Append(ctx, tx, store.Event{Stream: "landing:bifrost-platform", Type: "note", Actor: "controller", TaskID: task, MissionID: "M-3",
			Body: map[string]any{"text": "Landing retried: attempt " + attempt + " was accepted after the red CI of " + op,
				"landing": map[string]any{"v": landing.Version, "kind": "retry", "op": op, "root": task, "after_op": op,
					"reason": "rework: attempt " + attempt + " accepted after the red landing CI", "by": "controller"}}})
		return err
	}))
}

// AW, hold and release (Owner 2026-11-10, option 1): a wave's accepted
// members do not land while the wave is open; closing it releases them
// together, and the batch landing lands them with one landing CI and one
// push. A task outside the wave is not held.
func TestWaveHoldsThenLandsOnce(t *testing.T) {
	r := newBatchRig(t, false)
	w := r.openWave(t)
	var roots []string
	for _, p := range []string{"console/src/components/Wave0.tsx", "console/src/components/Wave1.tsx", "console/src/components/Wave2.tsx"} {
		roots = append(roots, r.acceptedIn(t, p, w.ID))
	}
	r.pass(t)
	if len(r.replays) != 0 || len(r.ensured) != 0 || len(r.batches(t)) != 0 {
		t.Fatal("an open wave's member was claimed", r.replays, r.ensured)
	}
	for _, root := range roots {
		if op := r.state(t).LatestOperation(root); op != "" {
			t.Fatal("held member has an operation", root, op)
		}
	}
	must(t, store.EndWave(ctx, r.pool, store.Writer{Actor: "thread:s1", Token: "operator"}, w.ID, false))
	b := r.formed(t, roots)
	if len(b.Members) != 3 {
		t.Fatal("the wave did not land as one batch", b)
	}
	if !slices.Equal(r.ensured, []string{landing.CIName(b.Lead, 1)}) {
		t.Fatal("CI kicked", r.ensured)
	}
	r.clock(time.Minute)
	r.status = "Succeeded"
	r.pass(t)
	if !slices.Equal(r.pushes, []string{b.R + "<-" + batchM0}) {
		t.Fatal("pushes", r.pushes)
	}
	if len(r.ensured) != 1 {
		t.Fatal("more than one landing CI", r.ensured)
	}
}

// A task proposed outside the wave lands as before while the wave is open.
func TestWaveDoesNotHoldOthers(t *testing.T) {
	r := newBatchRig(t, false)
	w := r.openWave(t)
	member := r.acceptedIn(t, "console/src/components/Held.tsx", w.ID)
	others := []string{r.accepted(t, "console/src/components/Free0.tsx"), r.accepted(t, "console/src/components/Free1.tsx")}
	b := r.formed(t, others)
	if len(b.Members) != 2 {
		t.Fatal("the tasks outside the wave did not land", b)
	}
	if r.state(t).LatestOperation(member) != "" {
		t.Fatal("the member was claimed while its wave is open")
	}
}

// A red wave sends the blamed member back; the others stay held (they do
// not land alone) until it is accepted again, then the wave lands as one
// batch on one more landing CI.
func TestWaveRedHoldsTheRestUntilReworked(t *testing.T) {
	r := newBatchRig(t, false)
	w := r.openWave(t)
	paths := []string{"tools/blame/a/a.go", "tools/blame/b/b.go", "tools/blame/c/c.go"}
	var roots []string
	for _, p := range paths {
		roots = append(roots, r.acceptedIn(t, p, w.ID))
	}
	must(t, store.EndWave(ctx, r.pool, store.Writer{Actor: "thread:s1", Token: "operator"}, w.ID, false))
	b := r.formed(t, roots)
	r.red(t, b, "--- FAIL: TestB (0.01s)\n    b_test.go:9: wrong\nFAIL\nFAIL\tgithub.com/weitingzhao/bifrost-platform/tools/blame/b\t0.02s\nok  \tgithub.com/weitingzhao/bifrost-platform/tools/blame/a\t0.01s\n")
	if r.taskState(t, roots[1]) != "ready" {
		t.Fatal("blamed member not back for rework", r.taskState(t, roots[1]))
	}
	before := len(r.batches(t))
	r.clock(time.Minute)
	r.pass(t)
	if len(r.batches(t)) != before {
		t.Fatal("the other members landed without the reworked one")
	}
	r.reaccept(t, roots[1], "tools/blame/b/b.go", 2)
	r.failure, r.status = "", "Running"
	r.clock(time.Minute)
	b2 := r.formed(t, []string{roots[0], roots[2], roots[1]})
	if len(b2.Members) != 3 {
		t.Fatal("the wave did not land again as one batch", b2)
	}
	r.clock(time.Minute)
	r.status = "Succeeded"
	r.pass(t)
	if len(r.pushes) != 1 || r.pushes[0] != b2.R+"<-"+batchM0 {
		t.Fatal("pushes", r.pushes)
	}
}
