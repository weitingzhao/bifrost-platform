package store_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Salvage (v5 §5.2; T34 ⑧, store half): only the host's own attempt, only
// past the grace; the result checked; persisted_at written while the state
// stays as it is; a later verification may accept the lost attempt.
func TestSalvage(t *testing.T) {
	w := newKernelWorld(t)
	w.ready(t, store.Task{})
	inGrace := w.claim(t, "h1", -time.Second, codex)
	report := w.artifact(t, "report", "# partial\n")
	calls := 0
	_, err := store.Salvage(ctx, w.rt, inGrace.AttemptID, "h1", store.Result{Report: report}, heads(nil, &calls))
	is(t, err, store.ErrInGrace)

	task := w.ready(t, store.Task{})
	old := w.claim(t, "h2", -store.Grace-time.Second, codex)
	head := heads(map[string]string{old.PushedRef: commitA}, &calls)
	_, err = store.Salvage(ctx, w.rt, old.AttemptID, "h3", store.Result{Report: report, Ref: old.PushedRef, Commit: commitA}, head)
	is(t, err, store.ErrFenced)
	_, err = store.Salvage(ctx, w.rt, old.AttemptID, "h2", store.Result{Report: report, Ref: "refs/heads/agent/" + task + "/a7", Commit: commitA},
		heads(map[string]string{"refs/heads/agent/" + task + "/a7": commitA}, &calls))
	is(t, err, store.ErrState)
	// Review 157 B1: salvage binds a report as persist does: a clean log
	// tail is not a report, and a report with a local path is refused.
	for _, bad := range []string{w.artifact(t, "log_tail", "compiling\n"), w.artifact(t, "review", "see /tmp/out\n")} {
		_, err = store.Salvage(ctx, w.rt, old.AttemptID, "h2", store.Result{Report: bad, Ref: old.PushedRef, Commit: commitA}, head)
		is(t, err, store.ErrState)
	}
	var unbound bool
	ok(t, w.rt.QueryRow(ctx, `SELECT persisted_at IS NULL AND report_artifact_id IS NULL FROM agentrt.attempt WHERE attempt_id = $1`,
		old.AttemptID).Scan(&unbound))
	if !unbound {
		t.Fatal("a refused salvage bound its result")
	}
	got, err := store.Salvage(ctx, w.rt, old.AttemptID, "h2", store.Result{Report: report, Ref: old.PushedRef, Commit: commitA}, head)
	ok(t, err)
	if got["persisted"] != true || w.state(t, "attempt", old.AttemptID) != "leased" || w.state(t, "task", task) != "leased" {
		t.Fatalf("salvage = %v; attempt %s, task %s", got, w.state(t, "attempt", old.AttemptID), w.state(t, "task", task))
	}
	_, _, err = store.SweepLeases(ctx, w.rt)
	ok(t, err)
	fresh := w.claim(t, "h4", store.LeaseTTL, codex)
	if fresh == nil || fresh.TaskID != task || w.state(t, "attempt", old.AttemptID) != "lost" {
		t.Fatalf("after the sweep: %+v", fresh)
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		ev, putErr := store.PutArtifact(ctx, tx, "controller", "verify", "text/plain", []byte("pass"))
		if putErr != nil {
			return putErr
		}
		_, putErr = tx.Exec(ctx, `INSERT INTO agentrt.verification (verification_id, task_id, attempt_id, method, verdict,
		  evidence_artifact_id, accepts_attempt, subject_commit) VALUES ($1,$2,$3,'deterministic','pass',$4,true,$5)`,
			store.NewVerificationID(), task, old.AttemptID, ev, commitA)
		return putErr
	}))
	if w.state(t, "attempt", old.AttemptID) != "lost" {
		t.Fatal("acceptance changed the lost attempt's state")
	}

	// Review 148 A1: an attempt that ended (failed) is still within its
	// lease and grace; the grace holds whatever the state.
	w.ready(t, store.Task{})
	ended := w.claim(t, "h5", store.LeaseTTL, codex)
	ok(t, store.Start(ctx, w.rt, fence(ended, "h5"), "", ""))
	_, err = store.Ended(ctx, w.rt, ended.AttemptID, "h5", "daemon_restart", true)
	ok(t, err)
	endedHead := heads(map[string]string{ended.PushedRef: commitA}, &calls)
	_, err = store.Salvage(ctx, w.rt, ended.AttemptID, "h5", store.Result{Report: report, Ref: ended.PushedRef, Commit: commitA}, endedHead)
	is(t, err, store.ErrInGrace)
	w.chainHolds(t, "mission:M-1")
}
