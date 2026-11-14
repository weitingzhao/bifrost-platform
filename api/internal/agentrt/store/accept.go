package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
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

// checkSpec is the task spec of a verification or review: the attempt it is
// about and what the checker needs, as canonical JSON.
type checkSpec struct {
	check, subject, attempt, commit, repo, taskSpec string
	round                                           int64
	verification, verifyReport                      string // a review's deterministic pass
	salvage                                         bool   // the attempt is a lost one whose result was salvaged (adopt.go)
}

// readCheckSpec reads the spec of a check; ok is false when it is not one
// the controller wrote (a check proposed by hand pins no attempt).
func readCheckSpec(ctx context.Context, tx pgx.Tx, artifactID string) (checkSpec, bool, error) {
	var body []byte
	if err := tx.QueryRow(ctx, `SELECT body FROM agentrt.artifact_blob WHERE artifact_id = $1`, artifactID).Scan(&body); err != nil {
		return checkSpec{}, false, err
	}
	v, err := canon.Parse(body)
	if err != nil {
		return checkSpec{}, false, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return checkSpec{}, false, nil
	}
	str := func(k string) string { s, _ := m[k].(string); return s }
	var c checkSpec
	c.check, c.subject, c.attempt, c.commit, c.repo, c.taskSpec = str("check"), str("subject_task"), str("attempt"), str("commit"), str("repo"), str("task_spec")
	c.verification, c.verifyReport = str("verification"), str("verify_report")
	c.salvage, _ = m["salvage"].(bool)
	n, ok := m["round"].(interface{ Int64() (int64, error) })
	if !ok || c.check == "" || c.subject == "" || c.attempt == "" {
		return checkSpec{}, false, nil
	}
	if c.round, err = n.Int64(); err != nil {
		return checkSpec{}, false, nil
	}
	return c, true, nil
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

// openCheck are the states of a verification or review that is still open:
// one that may yet be leased or used, a blocked one included (a pass blocks a
// check its mission's budget will not let run, TD-384; review 428 A1). Every
// test of "another check of the subject is open" reads this one list.
const openCheck = `('proposed','awaiting_owner','ready','leased','running','submitted','paused','blocked')`

// checkVersion says whether a check (spec, and vd its task) ran on what it
// pins and pins what the attempt is (review 152 A1): the attempt's own commit
// (pinnedCommit), the subject's repo and task spec (sd), and a checkout of
// that repo at that commit, or at the subject's base when the attempt pushed
// none. Conclude uses a check's result only when it does; stillChecked counts
// a check only when it does (review 432 A1).
func checkVersion(spec checkSpec, pinnedCommit string, sd, vd taskDetail) bool {
	base := pinnedCommit
	if base == "" {
		base = sd.base
	}
	return spec.commit == pinnedCommit && spec.repo == sd.repo && spec.taskSpec == sd.spec && vd.repo == sd.repo && vd.base == base
}

// stillChecked says whether an open check of s other than dropped can
// still accept s: it pins s itself, s's latest submitted attempt and s's
// round, is a check of its own role, and is not a salvaged result's (one
// cannot accept a task that is verifying), and its version is the attempt's
// (checkVersion); a review needs the attempt's deterministic pass. Not doing
// a check blocks s when none is left (reviews 428 A2, 430 A1, 432 A1: a check
// that only names the latest attempt, of an older round or another task spec,
// say, is closed by Conclude and accepts nothing).
// s and dropped are locked; the attempt is read without a lock (attempts
// come before tasks), and with s locked and verifying none of its attempts
// is submitted meanwhile.
func stillChecked(ctx context.Context, tx pgx.Tx, s taskRow, dropped string) (bool, error) {
	var latest, commit string
	err := tx.QueryRow(ctx, `SELECT attempt_id, COALESCE(pushed_commit, '') FROM agentrt.attempt WHERE task_id = $1 AND state = 'submitted'
	  ORDER BY attempt_no DESC LIMIT 1`, s.id).Scan(&latest, &commit)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	sd, err := readDetail(ctx, tx, s.id)
	if err != nil {
		return false, err
	}
	var passed bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.verification WHERE attempt_id = $1
	  AND method = 'deterministic' AND verdict = 'pass')`, latest).Scan(&passed); err != nil {
		return false, err
	}
	rows, err := tx.Query(ctx, `SELECT task_id, role FROM agentrt.task WHERE subject_task_id = $1 AND task_id <> $2
	  AND role IN ('verify','review') AND state IN `+openCheck, s.id, dropped)
	if err != nil {
		return false, err
	}
	type row struct{ task, role string }
	checks, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		return x, r.Scan(&x.task, &x.role)
	})
	if err != nil {
		return false, err
	}
	for _, c := range checks {
		vd, err := readDetail(ctx, tx, c.task)
		if err != nil {
			return false, err
		}
		spec, pinned, err := readCheckSpec(ctx, tx, vd.spec)
		if err != nil {
			return false, err
		}
		// What Conclude requires of a check before it uses its result.
		if pinned && !spec.salvage && spec.check == c.role && spec.subject == s.id && spec.attempt == latest &&
			spec.round == int64(sd.round) && checkVersion(spec, commit, sd, vd) && (c.role != "review" || passed) {
			return true, nil
		}
	}
	return false, nil
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
