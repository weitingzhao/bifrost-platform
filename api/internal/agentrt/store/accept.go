package store

import (
	"context"
	"errors"

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

var errNoSubmitted = errors.New("store: no submitted attempt")

// submittedAttempt locks the latest submitted attempt of a task.
func submittedAttempt(ctx context.Context, tx pgx.Tx, taskID string, mode string) (id, conclusion, report, commit string, err error) {
	var c, r, p *string
	err = tx.QueryRow(ctx, `SELECT attempt_id, conclusion, report_artifact_id, pushed_commit FROM agentrt.attempt
	  WHERE task_id = $1 AND state = 'submitted' ORDER BY attempt_no DESC LIMIT 1 `+mode, taskID).Scan(&id, &c, &r, &p)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", "", "", errNoSubmitted
	}
	for dst, src := range map[*string]*string{&conclusion: c, &report: r, &commit: p} {
		if src != nil {
			*dst = *src
		}
	}
	return id, conclusion, report, commit, err
}

type taskDetail struct {
	spec, offPlanReason, subject, repo, base string
	core                                     bool
	round, maxRounds                         int
}

func readDetail(ctx context.Context, tx pgx.Tx, taskID string) (taskDetail, error) {
	var d taskDetail
	err := tx.QueryRow(ctx, `SELECT spec_artifact_id, COALESCE(off_plan_reason, ''), COALESCE(subject_task_id, ''),
	  COALESCE(repo, ''), COALESCE(base_commit, ''), core, round_no, max_rounds
	  FROM agentrt.task WHERE task_id = $1`, taskID).Scan(&d.spec, &d.offPlanReason, &d.subject, &d.repo, &d.base, &d.core, &d.round, &d.maxRounds)
	return d, err
}

// lockStep takes a task's step FOR SHARE before anything else, as a plan
// change takes steps before tasks, and returns it ("" off the plan).
func lockStep(ctx context.Context, tx pgx.Tx, taskID string) (string, error) {
	var step string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(plan_node_id, '') FROM agentrt.task WHERE task_id = $1`, taskID).Scan(&step); err != nil {
		return "", err
	}
	if step != "" {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM agentrt.plan_node WHERE plan_node_id = $1 FOR SHARE`, step); err != nil {
			return "", err
		}
	}
	return step, nil
}

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
