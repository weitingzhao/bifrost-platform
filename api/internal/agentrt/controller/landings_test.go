package controller_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Review 436 B4: reading main for landings is a loop of its own: a Gitea that
// does not answer does not hold up dispatch while an accepted result waits to
// be looked for on main.
func TestSlowMainDoesNotHoldUpDispatch(t *testing.T) {
	pool := setup(t)
	var spec, evidence string
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		if err := store.MoveMission(ctx, tx, "controller", "M-1", "active", "test"); err != nil {
			return err
		}
		var err error
		if spec, err = store.PutArtifact(ctx, tx, "controller", "task_spec", "text/markdown", []byte("# t\n")); err != nil {
			return err
		}
		evidence, err = store.PutArtifact(ctx, tx, "controller", "report", "text/plain", []byte("pass\n"))
		return err
	}))
	base := strings.Repeat("b", 40)
	out, err := store.ProposeTasks(ctx, pool, store.Writer{Actor: "thread:s1", Token: "operator"}, "k1", "M-1", []store.TaskProposal{
		{Ref: "a", PlanNode: "R2:1-1", Role: "implement", Class: "go", Title: "a", SpecArtifact: spec, Effect: "branch",
			Repo: "bifrost-platform", BaseCommit: base},
		{Ref: "b", PlanNode: "R2:1-1", Role: "implement", Class: "go", Title: "b", SpecArtifact: spec, Effect: "branch",
			Repo: "bifrost-platform", BaseCommit: base}})
	must(t, err)
	tasks := out["tasks"].([]any)
	accepted, next := tasks[0].(map[string]any)["task_id"].(string), tasks[1].(map[string]any)["task_id"].(string)
	// Task a, accepted as the runtime leaves it: its attempt pushed, a
	// verification that accepts it.
	attempt := store.NewAttemptID()
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		// Written as an import, the one event that may write these rows at once.
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test"}}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.attempt (attempt_id, task_id, attempt_no, executor_actor_id, lease_epoch,
		    lease_expires_at, heartbeat_at, state, conclusion, pushed_ref, pushed_commit, computed_at, persisted_at, report_artifact_id)
		  VALUES ($1, $2, 1, 'controller', 1, now(), now(), 'finished', 'done', $3, $4, now(), now(), $5)`,
			attempt, accepted, store.AttemptRef(accepted, 1), strings.Repeat("a", 40), evidence); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.verification (verification_id, task_id, attempt_id, method, verdict,
		    evidence_artifact_id, accepts_attempt) VALUES ($1, $2, $3, 'review', 'pass', $4, true)`,
			store.NewVerificationID(), accepted, attempt, evidence); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.task SET state = 'accepted' WHERE task_id = $1`, accepted)
		return err
	}))

	src := (&world{}).sources()
	src.MainResults = func(ctx context.Context, _ string) (store.OnMain, bool) {
		<-ctx.Done() // Gitea does not answer
		return nil, false
	}
	c := &controller.Controller{DB: pool, Sources: src, LeaseEvery: time.Hour, FactsEvery: 20 * time.Millisecond}
	run, cancel := context.WithCancel(ctx)
	wait := c.Start(run)
	defer wait()
	defer cancel()
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		var s string
		must(t, pool.QueryRow(ctx, `SELECT state FROM agentrt.task WHERE task_id = $1`, next).Scan(&s))
		if s == "ready" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("dispatch waited on main: the task is still %s", s)
		}
	}
}
