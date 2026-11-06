package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// The landing CI as an implementation's check (R5:5.7-9, R5:5.7-10; the
// Owner 2026-11-05: 「为什么不最后统一验证，谁搞坏谁修」 and 「验证完了还 AI
// 复核，复核个嘚儿啊复核」). Before, a platform change was tested by its
// executor, by a deterministic verify task, by its landing CI and by the
// main-push CI, and a review task read it before it was accepted. The
// landing CI is the only full test of the commit that reaches main, so
// where the landing runs one it is the check:
//   - a submitted implementation whose landing runs a CI (landingCIOf) is
//     accepted on submission, with no verify task and no review: the
//     acceptance is a landing_ci verification, and its landing proceeds as
//     for any accepted attempt;
//   - where the landing runs no CI, the deterministic check stays and its
//     pass accepts the attempt, with no review;
//   - when the landing CI of an attempt accepted on submission is red
//     (failed{ci_red}) and the task has rounds left, the task goes back for
//     its next round instead of a root_failed card: the failure is recorded
//     as a failed landing_ci verification of that attempt (its failed
//     TaskRuns, as the landing CI reader saw them), which the next round's
//     claim names like a failed deterministic check (claimfailed.go). Once a
//     later attempt is accepted, the controller retries the landing
//     (retryReworkedLanding), as the Owner's try_again would.
// Precheck verify keeps what was before: the deterministic check, then the
// review, and a red landing CI is the Owner's card.

// Precheck is how a submitted implementation is checked (AGENTRT_PRECHECK).
type Precheck string

const (
	// PrecheckCI is the default: the landing CI where the landing runs one,
	// else the deterministic check; never a review.
	PrecheckCI Precheck = "ci"
	// PrecheckVerify is the check before R5:5.7-9: the deterministic check,
	// then a review.
	PrecheckVerify Precheck = "verify"
)

// ParsePrecheck reads AGENTRT_PRECHECK: unset is ci.
func ParsePrecheck(s string) (Precheck, error) {
	switch p := Precheck(strings.TrimSpace(s)); p {
	case "", PrecheckCI:
		return PrecheckCI, nil
	case PrecheckVerify:
		return p, nil
	}
	return PrecheckCI, fmt.Errorf("AGENTRT_PRECHECK is ci or verify, not %q", s)
}

// reviews says whether a passing deterministic check is followed by a
// review (precheck verify) rather than accepting the attempt.
func (p Precheck) reviews() bool { return p == PrecheckVerify }

// MethodLandingCI is the verification method of the landing CI: an
// acceptance on submission (pass, accepting) and a red landing CI (fail).
const MethodLandingCI = "landing_ci"

// landingCIRef is the check_ref of the failed landing_ci verification a red
// operation op records: the operation whose CI it was.
func landingCIRef(op string) string { return MethodLandingCI + ":" + op }

// landingCIOf is the pipeline of the CI the landing runs on what task's
// attempt pushed (commit), or "" when it runs none: the attempt pushed a
// commit; the task's repository is one the landing knows with a CI pipeline
// (rules.LandingRepositories); the landing rule in force for the mission's
// tenant names that repository; and the landing is on for it, its mission
// created no earlier than the repository's fence and not no_merge
// (LandingCandidates' own conditions), so an accepted attempt will be
// claimed and its CI run.
func landingCIOf(ctx context.Context, tx pgx.Tx, task, commit string) (string, error) {
	if commit == "" {
		return "", nil
	}
	var repo, tenant string
	var created time.Time
	var noMerge bool
	if err := tx.QueryRow(ctx, `SELECT COALESCE(t.repo, ''), m.tenant, m.created_at, COALESCE(m.scope_json->>'no_merge', '') <> ''
	  FROM agentrt.task t JOIN agentrt.mission m USING (mission_id) WHERE t.task_id = $1`, task).Scan(&repo, &tenant, &created, &noMerge); err != nil {
		return "", err
	}
	r, ok := rules.LandingRepositories[repo]
	if !ok || r.CIPipeline == "" || noMerge {
		return "", nil
	}
	if includes, err := landingRuleIncludes(ctx, tx, tenant, repo); err != nil || !includes {
		return "", err
	}
	s, err := LandingState(ctx, tx, "landing:"+repo)
	if err != nil {
		return "", err
	}
	if fence := s.Fence(); fence.IsZero() || created.Before(fence) {
		return "", nil
	}
	return r.CIPipeline, nil
}

// acceptOnSubmission accepts t's submitted attempt, whose landing runs
// pipeline: a landing_ci verification accepts it, and t (locked, submitted)
// is accepted.
func acceptOnSubmission(ctx context.Context, tx pgx.Tx, t taskRow, attempt, commit, pipeline string) error {
	body, err := json.Marshal(map[string]any{MethodLandingCI: map[string]any{"pipeline": pipeline, "accepted_on_submission": true,
		"text": "Accepted on submission: the landing CI " + pipeline + " tests this commit replayed on main; a red run sends the task back."}})
	if err != nil {
		return err
	}
	evidence, err := PutArtifact(ctx, tx, "controller", "verify", "application/json", body)
	if err != nil {
		return err
	}
	if err = recordLandingCI(ctx, tx, t, attempt, commit, "pass", evidence, ""); err != nil {
		return err
	}
	if err = moveTask(ctx, tx, "controller", t, "accepted", "accepted on submission: the landing CI "+pipeline+" is its check"); err != nil {
		return err
	}
	return retryReworkedLanding(ctx, tx, t, attempt)
}

// recordLandingCI records a landing_ci verification of t's attempt: a pass
// accepts it; a failure names the operation whose CI it was (ref).
func recordLandingCI(ctx context.Context, tx pgx.Tx, t taskRow, attempt, commit, verdict, evidence, ref string) error {
	vid := NewVerificationID()
	body := verificationBody(vid, MethodLandingCI, verdict, false)
	if ref != "" {
		body["check_ref"] = ref
	}
	if _, err := Append(ctx, tx, Event{Stream: MissionStream(t.mission), Type: "verification_recorded", Actor: "controller",
		MissionID: t.mission, PlanNode: t.step, TaskID: t.id, AttemptID: attempt, Body: body}); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO agentrt.verification (verification_id, task_id, attempt_id, method, verdict,
	  evidence_artifact_id, subject_commit, accepts_attempt, check_ref)
	  VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, NULLIF($9, ''))`, vid, t.id, attempt, MethodLandingCI, verdict,
		evidence, commit, verdict == "pass", ref); err != nil {
		return fmt.Errorf("store: record the landing CI: %w", err)
	}
	return nil
}

// landingCIReworked says whether the red CI of root's operation op sent
// root back for another round (a failed landing_ci verification names op).
// Such an operation has no root_failed card.
func landingCIReworked(ctx context.Context, tx pgx.Tx, root, op string) (bool, error) {
	var reworked bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.verification WHERE task_id = $1 AND method = $2
	  AND verdict = 'fail' AND check_ref = $3)`, root, MethodLandingCI, landingCIRef(op)).Scan(&reworked)
	return reworked, err
}

// retryReworkedLanding retries the landing of t, just accepted with
// attempt, when its latest operation is a red CI that sent it back for
// another round (landingCIReworked) and was another attempt's: the
// controller's retry record lets the next operation claim the new attempt,
// as the Owner's try_again would. t is locked; the repository stream is
// taken last.
func retryReworkedLanding(ctx context.Context, tx pgx.Tx, t taskRow, attempt string) error {
	var repo string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(repo, '') FROM agentrt.task WHERE task_id = $1`, t.id).Scan(&repo); err != nil {
		return err
	}
	if !landing.RepoSupported(repo) {
		return nil
	}
	stream := "landing:" + repo
	s, err := LandingState(ctx, tx, stream)
	if err != nil {
		return err
	}
	op := s.LatestOperation(t.id)
	o, ok := s.Operations[op]
	if !ok || o.Phase != "failed" || o.Outcome != "ci_red" || o.Retried || o.Attempt == attempt {
		return nil
	}
	if reworked, err := landingCIReworked(ctx, tx, t.id, op); err != nil || !reworked {
		return err
	}
	_, err = Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", TaskID: t.id, MissionID: t.mission,
		Body: map[string]any{"text": "Landing retried: attempt " + attempt + " was accepted after the red CI of " + op,
			"landing": map[string]any{"v": landing.Version, "kind": "retry", "op": op, "root": t.id, "after_op": op,
				"reason": "rework: attempt " + attempt + " accepted after the red landing CI", "by": "controller"}}})
	return err
}

// landingReworkMoves are the task moves of a red landing CI's rework: the
// accepted task goes back through rework to ready.
var landingReworkMoves = map[string][]string{"accepted": {"rework"}}

// RecordLandingCIRed ends repo's operation op failed{ci_red}, as the
// landing drill does (text is its note's text), and in the same
// transaction, when its root's attempt was accepted on submission and p is
// ci, sends the root back for its next round (R5:5.7-10): the failure, as
// the landing CI reader saw it, becomes the failed landing_ci check of that
// attempt, which the next round's claim names; the root goes back to ready
// in round + 1. It does not when the root has no round left, its mission is
// no longer active or no_merge, its step is closed, it is stopped, or the
// operation is not the one of its accepted attempt: the operation fails as
// before and the Owner gets its root_failed card. It returns whether the
// root went back.
//
// Lock order: the step, the task, the mission's stream, then the
// repository's stream last.
func RecordLandingCIRed(ctx context.Context, db DB, repo, op, text string, failure landing.CIFailure, p Precheck) (bool, error) {
	stream := "landing:" + repo
	var reworked bool
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		reworked = false
		s, err := LandingState(ctx, tx, stream)
		if err != nil {
			return err
		}
		o, ok := s.Operations[op]
		if !ok || o.Task == "" {
			return fmt.Errorf("unknown landing operation %s", op)
		}
		if !p.reviews() && s.LatestOperation(o.Task) == op && !o.Terminal() {
			if reworked, err = reworkRedLanding(ctx, tx, o, op, failure); err != nil {
				return err
			}
		}
		_, err = Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller",
			Body: map[string]any{"text": text, "landing": map[string]any{"v": landing.Version, "kind": "failed", "op": op, "why": "ci_red"}}})
		return err
	})
	return reworked, err
}

// reworkRedLanding sends o's root back for its next round when it may go
// (RecordLandingCIRed), and says whether it went.
func reworkRedLanding(ctx context.Context, tx pgx.Tx, o landing.Operation, op string, failure landing.CIFailure) (bool, error) {
	step, err := lockStep(ctx, tx, o.Task)
	if err != nil {
		return false, err
	}
	t, err := readTask(ctx, tx, o.Task, true)
	if err != nil || t.state != "accepted" || t.role != "implement" || t.step != step {
		return false, err
	}
	d, err := readDetail(ctx, tx, t.id)
	if err != nil || d.round+1 > d.maxRounds {
		return false, err
	}
	var accepted, method string
	err = tx.QueryRow(ctx, `SELECT attempt_id, method FROM agentrt.verification WHERE task_id = $1 AND accepts_attempt
	  ORDER BY created_at DESC, verification_id DESC LIMIT 1`, t.id).Scan(&accepted, &method)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (accepted != o.Attempt || method != MethodLandingCI) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if open, err := rootMissionOpen(ctx, tx, t.id); err != nil || !open {
		return false, err
	}
	if step != "" {
		if open, err := reworkStepOpen(ctx, tx, step); err != nil || !open {
			return false, err
		}
	}
	if stopped, err := held(ctx, tx, []scope{{"task", t.id}}, "stop"); err != nil || stopped {
		return false, err
	}
	body, err := json.Marshal(map[string]any{MethodLandingCI: map[string]any{"op": op, "R": o.R, "pipeline": failure.Pipeline,
		"run": failure.Run, "task_runs": failedTasks(failure.TaskRuns), "read_error": failure.ReadError}})
	if err != nil {
		return false, err
	}
	evidence, err := PutArtifact(ctx, tx, "controller", "log_tail", "application/json", body)
	if err != nil {
		return false, err
	}
	if err = recordLandingCI(ctx, tx, t, o.Attempt, o.A, "fail", evidence, landingCIRef(op)); err != nil {
		return false, err
	}
	why := fmt.Sprintf("the landing CI of %s failed in round %d of %d", op, d.round, d.maxRounds)
	if err = moveTaskBy(ctx, tx, "controller", t, "rework", why, landingReworkMoves); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `UPDATE agentrt.task SET round_no = round_no + 1 WHERE task_id = $1`, t.id); err != nil {
		return false, err
	}
	t.state = "rework"
	return true, moveTask(ctx, tx, "controller", t, "ready", fmt.Sprintf("round %d", d.round+1))
}

func failedTasks(tasks []landing.CIFailedTask) []landing.CIFailedTask {
	if tasks == nil {
		return []landing.CIFailedTask{}
	}
	return tasks
}
