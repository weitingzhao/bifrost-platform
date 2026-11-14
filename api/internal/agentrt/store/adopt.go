package store

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
)

// Accepting a salvaged result (design v5 §5.2 /salvage, T34 case 8; review
// 152 A3; the Owner chose option A on 2026-10-14). An attempt that lost its
// lease stays lost; once the grace has passed its host may salvage its
// result, which persists it (Salvage). The controller checks that result as
// it checks a submitted one: a deterministic verification pinned to the lost
// attempt, then, with precheck verify, a review, their specs marked salvage.
// The pass that accepts (the review's, else the deterministic check's)
// accepts the lost attempt, and the same transaction cancels the task's live
// new attempt, if there is one (attempt_cancelled), and accepts the task:
// one accepted attempt, nothing left running for it. The task does not move
// to verifying meanwhile: its new attempt keeps running, and a salvaged
// result that fails its check moves nothing.
//
// A salvaged result is checked only while it can still be the task's result:
// the task is ready, leased or running (not paused or blocked, and not past a
// submit), no later attempt was submitted, the attempt has no verification
// yet, and it is checked in the round it was pinned in. Whichever comes first
// wins: a new attempt that submits before the acceptance closes the salvage
// check; an acceptance that comes first fences the new attempt, whose next
// call is refused, and its host ends it.

// adoptMoves are the task moves of accepting a salvaged result.
var adoptMoves = map[string][]string{"ready": {"accepted"}, "leased": {"accepted"}, "running": {"accepted"}}

var adoptable = []string{"ready", "leased", "running"}

// liveAttempt is the task's live attempt ("" when it has none), with lock.
func liveAttempt(ctx context.Context, tx pgx.Tx, taskID, lock string) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT attempt_id FROM agentrt.attempt WHERE task_id = $1 AND state IN ('leased','running','stale') `+lock,
		taskID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// errLiveMoved: the subject's live attempt is not the one locked before the
// subject was; the next pass looks again, with the new one locked in order.
var errLiveMoved = errors.New("store: the subject's live attempt changed")

// salvageStale says why a check of a salvaged result is no longer about what
// the task can accept ("" when it still is), with the subject locked and
// live its live attempt as locked before. The spec's salvage mark is only a
// claim: every condition of discovery (salvagedOpen) is checked again here,
// on the state at this moment, so a check proposed by hand, or one that
// waited while the task moved on, is judged as the controller's own would
// be (reviews 166 A1, 168 A1), as an ordinary pin is judged by the state
// alone: the subject is an implementation in the round pinned and ready,
// leased or running; the attempt is lost and persisted; no later attempt
// was submitted; no other check of the subject is open (checker is this
// one); and the attempt's verifications are this chain's own (none before
// its deterministic check; that check's pass alone before its review).
func salvageStale(ctx context.Context, tx pgx.Tx, spec checkSpec, s taskRow, sd taskDetail, checker, pinnedState, live string) (string, error) {
	switch {
	case s.role != "implement":
		return "the subject is a " + s.role + " task, not an implementation", nil
	case pinnedState != "lost":
		return fmt.Sprintf("the salvaged attempt %s is %q, not lost", spec.attempt, pinnedState), nil
	case int64(sd.round) != spec.round:
		return fmt.Sprintf("stale: about round %d; the subject is in round %d", spec.round, sd.round), nil
	case !slices.Contains(adoptable, s.state):
		return "the subject is " + s.state + ": its own attempt's result goes first", nil
	}
	var persisted, later, other bool
	var checks, chain int
	if err := tx.QueryRow(ctx, `SELECT a.persisted_at IS NOT NULL,
	  EXISTS (SELECT 1 FROM agentrt.attempt b WHERE b.task_id = a.task_id AND b.attempt_no > a.attempt_no AND b.state IN ('submitted','finished')),
	  EXISTS (SELECT 1 FROM agentrt.task c WHERE c.subject_task_id = a.task_id AND c.task_id <> $3 AND c.role IN ('verify','review')
	          AND c.state IN `+openCheck+`),
	  (SELECT count(*) FROM agentrt.verification v WHERE v.attempt_id = a.attempt_id),
	  (SELECT count(*) FROM agentrt.verification v WHERE v.attempt_id = a.attempt_id AND v.verification_id = $2
	     AND v.method = 'deterministic' AND v.verdict = 'pass')
	  FROM agentrt.attempt a WHERE a.attempt_id = $1`, spec.attempt, spec.verification, checker).Scan(&persisted, &later, &other, &checks, &chain); err != nil {
		return "", err
	}
	switch {
	case !persisted:
		return "the attempt's result was not salvaged", nil
	case later:
		return "a later attempt was submitted", nil
	case other:
		return "another check of the subject is open", nil
	case spec.check == "verify" && checks != 0:
		return "the attempt was checked before", nil
	case spec.check == "review" && (checks != 1 || chain != 1):
		return "the attempt's checks are not this review's deterministic pass alone", nil
	}
	now, err := liveAttempt(ctx, tx, s.id, "")
	if err != nil {
		return "", err
	}
	if now != live {
		return "", errLiveMoved
	}
	return "", nil
}

// adopt accepts the salvaged attempt, whose check (role) passed: the
// subject's live attempt (locked before the subject) is cancelled, and the
// subject is accepted.
func adopt(ctx context.Context, tx pgx.Tx, s taskRow, accepted, live, role string) error {
	if live != "" {
		if err := attemptEvent(ctx, tx, "controller", attemptRow{id: live, task: s.id}, "attempt_cancelled",
			map[string]any{"reason": "the salvaged result of attempt " + accepted + " was accepted", "accepted_attempt": accepted}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE agentrt.attempt SET state = 'cancelled' WHERE attempt_id = $1`, live); err != nil {
			return err
		}
		hook("cancelled", live)
	}
	passed := "review"
	if role == "verify" {
		passed = "deterministic verification"
	}
	return moveTaskBy(ctx, tx, "controller", s, "accepted", "the salvaged result of attempt "+accepted+" passed "+passed, adoptMoves)
}
