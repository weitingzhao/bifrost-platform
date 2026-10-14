package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

// Acceptance (design v5 §5.2 控制器「验收编排」). A submitted implementation
// goes to verifying and gets a deterministic verification task; a passing
// verification gets a review task (routed at claim time, where independence
// is checked again under the subject's lock); a passing review records the
// verification that accepts the implementation's attempt, and the
// implementation is accepted. A failure sends the implementation back for
// rework while rounds remain (round_no + 1 <= max_rounds), else blocks it.
// Verification and review tasks are accepted once their result is used;
// they are not verified in turn.
//
// The verdict of a verification or review attempt is its conclusion: done
// passes; failed or none fails. Attempts stay submitted after their result
// is used: the event vocabulary has no attempt event for "finished", and the
// verification rows are the record of the outcome.
//
// Lock order here, as in the kernel: attempts (the verifying task's, then the
// subject's), then tasks (the verifying task before its subject), then
// streams.

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
	spec, offPlanReason, subject string
	core                         bool
	round, maxRounds             int
}

func readDetail(ctx context.Context, tx pgx.Tx, taskID string) (taskDetail, error) {
	var d taskDetail
	err := tx.QueryRow(ctx, `SELECT spec_artifact_id, COALESCE(off_plan_reason, ''), COALESCE(subject_task_id, ''), core, round_no, max_rounds
	  FROM agentrt.task WHERE task_id = $1`, taskID).Scan(&d.spec, &d.offPlanReason, &d.subject, &d.core, &d.round, &d.maxRounds)
	return d, err
}

// proposeChecker proposes a verification or review task of subject on the
// subject's step and makes it ready to claim.
func proposeChecker(ctx context.Context, tx pgx.Tx, subject taskRow, d taskDetail, role, spec string) (string, error) {
	id := NewTaskID()
	if _, err := ProposeTask(ctx, tx, "controller", Task{ID: id, MissionID: subject.mission, PlanNode: subject.step,
		OffPlanReason: d.offPlanReason, Subject: subject.id, Role: role, Class: subject.class, Title: role + " " + subject.id,
		SpecArtifact: spec, Effect: "none", Core: d.core}); err != nil {
		return "", err
	}
	return id, MarkReady(ctx, tx, "controller", id)
}

// StartVerification moves a submitted implementation to verifying and
// proposes its deterministic verification task, ready to claim, which
// carries the implementation's own task spec (the commands to run). It
// returns the new task's id, or "" when the task is not a submitted
// implementation.
func StartVerification(ctx context.Context, db DB, taskID string) (string, error) {
	var id string
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		if _, _, _, _, err := submittedAttempt(ctx, tx, taskID, "FOR SHARE"); err != nil {
			if errors.Is(err, errNoSubmitted) {
				return nil
			}
			return err
		}
		t, err := readTask(ctx, tx, taskID, true)
		if err != nil || t.state != "submitted" || t.role != "implement" {
			return err
		}
		d, err := readDetail(ctx, tx, taskID)
		if err != nil {
			return err
		}
		if err = moveTask(ctx, tx, "controller", t, "verifying", "deterministic verification"); err != nil {
			return err
		}
		t.state = "verifying"
		id, err = proposeChecker(ctx, tx, t, d, "verify", d.spec)
		return err
	})
	return id, err
}

// Conclude uses the result of a verification or review task whose attempt
// was submitted: it records the verification of the subject's submitted
// attempt (verification_recorded) and moves the subject on. It returns what
// it did: "review" (a review task was proposed), "accepted", "rework",
// "blocked", "closed" (the subject was no longer under verification), or ""
// when there was nothing to do.
func Conclude(ctx context.Context, db DB, taskID string) (string, error) {
	var did string
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		d, err := readDetail(ctx, tx, taskID)
		if err != nil {
			return err
		}
		va, conclusion, report, _, err := submittedAttempt(ctx, tx, taskID, "FOR UPDATE")
		if errors.Is(err, errNoSubmitted) {
			return nil
		}
		if err != nil {
			return err
		}
		sa, _, _, commit, err := submittedAttempt(ctx, tx, d.subject, "FOR SHARE")
		if err != nil && !errors.Is(err, errNoSubmitted) {
			return err
		}
		v, err := readTask(ctx, tx, taskID, true)
		if err != nil {
			return err
		}
		if v.state != "submitted" || (v.role != "verify" && v.role != "review") {
			return nil
		}
		s, err := readTask(ctx, tx, d.subject, true)
		if err != nil {
			return err
		}
		sd, err := readDetail(ctx, tx, s.id)
		if err != nil {
			return err
		}
		if s.state != "verifying" || sa == "" {
			did = "closed"
			return moveTask(ctx, tx, "controller", v, "accepted", "the subject is no longer under verification")
		}
		verdict, method := "fail", "deterministic"
		if conclusion == "done" {
			verdict = "pass"
		}
		if v.role == "review" {
			method = "review"
		}
		accepts := v.role == "review" && verdict == "pass"
		vid := NewVerificationID()
		if _, err = Append(ctx, tx, Event{Stream: MissionStream(s.mission), Type: "verification_recorded", Actor: "controller",
			MissionID: s.mission, PlanNode: s.step, TaskID: s.id, AttemptID: sa,
			Body: map[string]any{"verification_id": vid, "method": method, "verdict": verdict}}); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO agentrt.verification (verification_id, task_id, attempt_id, method, verdict,
		  evidence_artifact_id, verifier_attempt_id, subject_commit, accepts_attempt)
		  VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), $9)`, vid, s.id, sa, method, verdict, report, va, commit, accepts); err != nil {
			return fmt.Errorf("store: record verification: %w", err)
		}
		if err = moveTask(ctx, tx, "controller", v, "accepted", "result used: "+verdict); err != nil {
			return err
		}
		switch {
		case accepts:
			did = "accepted"
			return moveTask(ctx, tx, "controller", s, "accepted", "review passed")
		case verdict == "pass":
			var brief string
			if brief, err = reviewBrief(ctx, tx, s, sa, commit, sd.spec, vid, report); err != nil {
				return err
			}
			did = "review"
			_, err = proposeChecker(ctx, tx, s, sd, "review", brief)
			return err
		}
		if sd.round+1 > sd.maxRounds {
			did = "blocked"
			return moveTask(ctx, tx, "controller", s, "blocked", fmt.Sprintf("%s failed in round %d of %d", method, sd.round, sd.maxRounds))
		}
		did = "rework"
		if err = moveTask(ctx, tx, "controller", s, "rework", fmt.Sprintf("%s failed in round %d of %d", method, sd.round, sd.maxRounds)); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE agentrt.task SET round_no = round_no + 1 WHERE task_id = $1`, s.id); err != nil {
			return err
		}
		s.state = "rework"
		return moveTask(ctx, tx, "controller", s, "ready", fmt.Sprintf("round %d", sd.round+1))
	})
	return did, err
}

// reviewBrief is the task spec of a review: what to review and the evidence
// so far, as canonical JSON.
func reviewBrief(ctx context.Context, tx pgx.Tx, s taskRow, attempt, commit, spec, verification, report string) (string, error) {
	doc := map[string]any{"review_of": s.id, "attempt": attempt, "task_spec": spec, "verification": verification, "verify_report": report}
	if commit != "" {
		doc["commit"] = commit
	}
	b, err := canon.Encode(doc)
	if err != nil {
		return "", err
	}
	return PutArtifact(ctx, tx, "controller", "task_spec", "application/json", b)
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
