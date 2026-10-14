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

func (c checkSpec) put(ctx context.Context, tx pgx.Tx) (string, error) {
	doc := map[string]any{"check": c.check, "subject_task": c.subject, "attempt": c.attempt, "round": c.round, "task_spec": c.taskSpec}
	if c.salvage {
		doc["salvage"] = true
	}
	for k, v := range map[string]string{"commit": c.commit, "repo": c.repo, "verification": c.verification, "verify_report": c.verifyReport} {
		if v != "" {
			doc[k] = v
		}
	}
	b, err := canon.Encode(doc)
	if err != nil {
		return "", err
	}
	return PutArtifact(ctx, tx, "controller", "task_spec", "application/json", b)
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

// proposeChecker proposes a verification or review of subject on the
// subject's step, starting from its repository at the attempt's commit, and
// makes it ready to claim.
func proposeChecker(ctx context.Context, tx pgx.Tx, subject taskRow, d taskDetail, spec checkSpec) (string, error) {
	specID, err := spec.put(ctx, tx)
	if err != nil {
		return "", err
	}
	base := spec.commit
	if base == "" {
		base = d.base
	}
	id := NewTaskID()
	if _, err := ProposeTask(ctx, tx, "controller", Task{ID: id, MissionID: subject.mission, PlanNode: subject.step,
		OffPlanReason: d.offPlanReason, Subject: subject.id, Role: spec.check, Class: subject.class, Title: spec.check + " " + subject.id,
		SpecArtifact: specID, Effect: "none", Core: d.core, Repo: d.repo, BaseCommit: base}); err != nil {
		return "", err
	}
	return id, MarkReady(ctx, tx, "controller", id)
}

// StartVerification moves a submitted implementation to verifying and
// proposes its deterministic verification task, ready to claim, pinned to
// the submitted attempt. It returns the new task's id, or "" when the task
// is not a submitted implementation (or moved while this ran: the next pass
// tries again).
func StartVerification(ctx context.Context, db DB, taskID string) (string, error) {
	var id string
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		step, err := lockStep(ctx, tx, taskID)
		if err != nil {
			return err
		}
		sa, _, _, commit, err := submittedAttempt(ctx, tx, taskID, "FOR SHARE")
		if errors.Is(err, errNoSubmitted) {
			return nil
		}
		if err != nil {
			return err
		}
		hook("verifying", taskID)
		t, err := readTask(ctx, tx, taskID, true)
		if err != nil || t.state != "submitted" || t.role != "implement" || t.step != step {
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
		id, err = proposeChecker(ctx, tx, t, d, checkSpec{check: "verify", subject: t.id, attempt: sa, round: int64(d.round),
			commit: commit, repo: d.repo, taskSpec: d.spec})
		return err
	})
	return id, err
}

// Conclude uses the result of a verification or review task whose attempt
// was submitted. A result about the subject's current attempt and round
// records the verification (verification_recorded) and moves the subject
// on; any other closes the check and moves nothing. A check of a salvaged
// result goes by adopt.go: a pass of its review accepts the lost attempt and
// cancels the live one; a failure moves nothing. It returns what it did:
// "review" (a review task was proposed), "accepted", "rework", "blocked",
// "closed", or "" when there was nothing to do.
func Conclude(ctx context.Context, db DB, taskID string) (string, error) {
	var did string
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		var role, specID, subject string
		if err := tx.QueryRow(ctx, `SELECT role, spec_artifact_id, COALESCE(subject_task_id, '') FROM agentrt.task WHERE task_id = $1`,
			taskID).Scan(&role, &specID, &subject); err != nil {
			return err
		}
		if role != "verify" && role != "review" {
			return nil
		}
		step, err := lockStep(ctx, tx, taskID)
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
		spec, pinned, err := readCheckSpec(ctx, tx, specID)
		if err != nil {
			return err
		}
		var pinnedState, pinnedCommit string
		if pinned {
			err = tx.QueryRow(ctx, `SELECT state, COALESCE(pushed_commit, '') FROM agentrt.attempt WHERE attempt_id = $1 AND task_id = $2 FOR SHARE`,
				spec.attempt, subject).Scan(&pinnedState, &pinnedCommit)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		// Accepting a salvaged result cancels the subject's live attempt:
		// it is locked here, with the subject's attempts, and checked again
		// once the subject is locked (adopt.go).
		var live string
		if pinned && spec.salvage {
			if live, err = liveAttempt(ctx, tx, subject, "FOR UPDATE"); err != nil {
				return err
			}
			hook("adopting", subject)
		}
		current, _, _, _, err := submittedAttempt(ctx, tx, subject, "FOR SHARE")
		if err != nil && !errors.Is(err, errNoSubmitted) {
			return err
		}
		s, err := readTask(ctx, tx, subject, true)
		if err != nil {
			return err
		}
		v, err := readTask(ctx, tx, taskID, true)
		if err != nil {
			return err
		}
		if v.state != "submitted" || v.step != step || s.step != step {
			return nil
		}
		sd, err := readDetail(ctx, tx, s.id)
		if err != nil {
			return err
		}
		vd, err := readDetail(ctx, tx, v.id)
		if err != nil {
			return err
		}
		// A check that is not about the attempt under verification closes and
		// moves nothing; when it was the subject's only check, a new pinned
		// verification takes its place (review 152 A2).
		closeCheck := func(reason string) error {
			did = "closed"
			if moveErr := moveTask(ctx, tx, "controller", v, "accepted", reason); moveErr != nil {
				return moveErr
			}
			return ensureCheck(ctx, tx, s, sd, v.id)
		}
		base := pinnedCommit
		if base == "" {
			base = sd.base
		}
		stale := ""
		if pinned && spec.salvage && spec.check == v.role && spec.subject == s.id {
			stale, err = salvageStale(ctx, tx, spec, s, sd, pinnedState, live)
			if errors.Is(err, errLiveMoved) {
				return nil
			}
			if err != nil {
				return err
			}
		}
		switch {
		case !pinned || spec.check != v.role || spec.subject != s.id:
			return closeCheck("the check pins no attempt of its subject")
		case spec.salvage && stale != "":
			return closeCheck(stale)
		case !spec.salvage && (pinnedState != "submitted" || spec.attempt != current || s.state != "verifying" || int64(sd.round) != spec.round):
			return closeCheck(fmt.Sprintf("stale: about attempt %s of round %d; the subject is %s in round %d", spec.attempt, spec.round, s.state, sd.round))
		// The check ran on what it pins, and pins what the attempt is (review
		// 152 A1): the attempt's own commit, the subject's repo and task
		// spec, and a checkout of that repo at that commit.
		case spec.commit != pinnedCommit || spec.repo != sd.repo || spec.taskSpec != sd.spec || vd.repo != sd.repo || vd.base != base:
			return closeCheck("the check's version is not the attempt's")
		case v.role == "review":
			var passed bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.verification WHERE attempt_id = $1
			  AND method = 'deterministic' AND verdict = 'pass')`, spec.attempt).Scan(&passed); err != nil {
				return err
			}
			if !passed {
				return closeCheck("no deterministic pass of attempt " + spec.attempt)
			}
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
			MissionID: s.mission, PlanNode: s.step, TaskID: s.id, AttemptID: spec.attempt,
			Body: map[string]any{"verification_id": vid, "method": method, "verdict": verdict}}); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO agentrt.verification (verification_id, task_id, attempt_id, method, verdict,
		  evidence_artifact_id, verifier_attempt_id, subject_commit, accepts_attempt)
		  VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), $9)`, vid, s.id, spec.attempt, method, verdict, report, va, spec.commit, accepts); err != nil {
			return fmt.Errorf("store: record verification: %w", err)
		}
		if err = moveTask(ctx, tx, "controller", v, "accepted", "result used: "+verdict); err != nil {
			return err
		}
		switch {
		case accepts && spec.salvage:
			did = "accepted"
			return adopt(ctx, tx, s, spec.attempt, live)
		case accepts:
			did = "accepted"
			return moveTask(ctx, tx, "controller", s, "accepted", "review passed")
		case verdict == "pass":
			did = "review"
			review := spec
			review.check, review.verification, review.verifyReport = "review", vid, report
			_, err = proposeChecker(ctx, tx, s, sd, review)
			return err
		case spec.salvage:
			did = "closed" // a salvaged result that fails moves nothing: the task goes on with its own attempts
			return nil
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

// ensureCheck proposes a pinned verification of the subject's attempt under
// verification when the subject is verifying and no other check of it is
// still open (except is the one being closed).
func ensureCheck(ctx context.Context, tx pgx.Tx, s taskRow, sd taskDetail, except string) error {
	if s.state != "verifying" {
		return nil
	}
	var open bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.task WHERE subject_task_id = $1 AND task_id <> $2
	  AND role IN ('verify','review') AND state IN ('proposed','awaiting_owner','ready','leased','running','submitted','paused'))`,
		s.id, except).Scan(&open); err != nil || open {
		return err
	}
	sa, _, _, commit, err := submittedAttempt(ctx, tx, s.id, "FOR SHARE")
	if errors.Is(err, errNoSubmitted) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = proposeChecker(ctx, tx, s, sd, checkSpec{check: "verify", subject: s.id, attempt: sa, round: int64(sd.round),
		commit: commit, repo: sd.repo, taskSpec: sd.spec})
	return err
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
