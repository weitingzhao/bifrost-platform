package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Acceptance (design v5 §5.2 控制器「验收编排」; R5:5.7-9, precheck.go). By
// default (precheck ci) a submitted implementation whose landing runs a CI
// is accepted on submission, the landing CI its check; any other goes to
// verifying and gets a deterministic verification task, whose pass records
// the verification that accepts the attempt, and the implementation is
// accepted. With precheck verify, a passing verification gets a review task
// instead (routed at claim time, where independence is checked again under
// the subject's lock), and only a passing review accepts. A failure sends
// the implementation back for rework while rounds remain (round_no + 1 <=
// max_rounds), else blocks it. Verification and review tasks are accepted
// once their result is used; they are not verified in turn.
//
// A check is about one attempt: its task spec pins the subject's attempt,
// round and commit, and its claim starts from the subject's repository at
// that commit. A result is used only for the attempt it pins, while the
// subject is still in that round and under verification; a review is used
// only after a deterministic pass of the same attempt. Any other result
// closes its check and moves nothing (review 151 A3, A4).
//
// The verdict of a check is its attempt's conclusion: done passes; failed or
// none fails. Attempts stay submitted after their result is used: the event
// vocabulary has no attempt event for "finished", and the verification rows
// are the record of the outcome.
//
// Lock order here: the step first, as a plan change takes it (review 151
// A2); then the attempts (the check's, then the subject's); then the tasks,
// the subject before its check (a claim, which locks the check first, takes
// the subject with SKIP LOCKED and so never waits for it); then streams.

// TestNeedFailFirstVerify is a need a task may carry to show the rework path
// on STG (T26; the Owner 2026-10-18): where the controller runs with
// ConcludeOptions.FailFirstVerify (STG only), the task's first deterministic
// verification records a failure whatever it found, so the task goes round
// again. Elsewhere it is a need nothing holds a fact on, and does nothing.
const TestNeedFailFirstVerify = "test:fail-first-verify"

// ConcludeOptions are the controller's switches.
type ConcludeOptions struct {
	// FailFirstVerify honours TestNeedFailFirstVerify (STG only).
	FailFirstVerify bool
	// Precheck is how an implementation is checked (precheck_mode.go).
	Precheck Precheck
}

// Pending lists the tasks the acceptance pass has to look at: submitted
// implementations, and verification and review tasks with a submitted
// result.
func Pending(ctx context.Context, db DB) ([]string, []string, error) {
	var implementations, checks []string
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT task_id, role FROM agentrt.task WHERE state = 'submitted' AND role IN ('implement','verify','review')
		  ORDER BY updated_at, task_id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, role string
			if err = rows.Scan(&id, &role); err != nil {
				return err
			}
			if role == "implement" {
				implementations = append(implementations, id)
			} else {
				checks = append(checks, id)
			}
		}
		return rows.Err()
	})
	return implementations, checks, err
}
