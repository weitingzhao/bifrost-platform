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
// attempt, then a review, their specs marked salvage. A passing review
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

// salvagedOpen is the condition on attempt a (and its task t) under which its
// salvaged result is still to be checked: not while a check of t is open (a
// blocked one included). Discovery and StartSalvageCheck then pass over an
// attempt whose check the Owner chose not to do (droppedPin), else every pass
// would make a new check and a new card for the result he dropped (review
// 428 A1).
const salvagedOpen = `a.state = 'lost' AND a.persisted_at IS NOT NULL AND t.role = 'implement' AND t.state IN ('ready','leased','running')
  AND NOT EXISTS (SELECT 1 FROM agentrt.attempt b WHERE b.task_id = a.task_id AND b.attempt_no > a.attempt_no
                  AND b.state IN ('submitted','finished'))
  AND NOT EXISTS (SELECT 1 FROM agentrt.verification v WHERE v.attempt_id = a.attempt_id)
  AND NOT EXISTS (SELECT 1 FROM agentrt.task c WHERE c.subject_task_id = t.task_id AND c.role IN ('verify','review')
                  AND c.state IN ` + openCheck + `)`

// PendingSalvaged lists the lost attempts whose salvaged result the
// acceptance pass is to check.
func PendingSalvaged(ctx context.Context, db DB) ([]string, error) {
	var ids []string
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT a.attempt_id, a.task_id FROM agentrt.attempt a JOIN agentrt.task t ON t.task_id = a.task_id
		  WHERE `+salvagedOpen+` ORDER BY a.persisted_at, a.attempt_id`)
		if err != nil {
			return err
		}
		type pair struct{ attempt, task string }
		found, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (pair, error) {
			var p pair
			return p, r.Scan(&p.attempt, &p.task)
		})
		if err != nil {
			return err
		}
		ids = ids[:0]
		for _, p := range found {
			dropped, err := droppedPin(ctx, tx, p.task, p.attempt)
			if err != nil {
				return err
			}
			if !dropped {
				ids = append(ids, p.attempt)
			}
		}
		return nil
	})
	return ids, err
}

// StartSalvageCheck proposes the deterministic verification of a lost
// attempt's salvaged result, pinned to that attempt and marked salvage. It
// returns the new task's id, or "" when the result is no longer to be checked
// (or another pass did it).
func StartSalvageCheck(ctx context.Context, db DB, attemptID string) (string, error) {
	var id string
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		var taskID string
		if err := tx.QueryRow(ctx, `SELECT task_id FROM agentrt.attempt WHERE attempt_id = $1`, attemptID).Scan(&taskID); err != nil {
			return err
		}
		step, err := lockStep(ctx, tx, taskID)
		if err != nil {
			return err
		}
		var commit *string
		if err = tx.QueryRow(ctx, `SELECT pushed_commit FROM agentrt.attempt WHERE attempt_id = $1 FOR SHARE`, attemptID).Scan(&commit); err != nil {
			return err
		}
		t, err := readTask(ctx, tx, taskID, true)
		if err != nil || t.step != step {
			return err
		}
		hook("salvage-check", taskID)
		var open bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.attempt a JOIN agentrt.task t ON t.task_id = a.task_id
		  WHERE a.attempt_id = $1 AND `+salvagedOpen+`)`, attemptID).Scan(&open); err != nil || !open {
			return err
		}
		if dropped, err := droppedPin(ctx, tx, taskID, attemptID); err != nil || dropped {
			return err
		}
		d, err := readDetail(ctx, tx, taskID)
		if err != nil {
			return err
		}
		spec := checkSpec{check: "verify", subject: t.id, attempt: attemptID, round: int64(d.round), repo: d.repo, taskSpec: d.spec, salvage: true}
		if commit != nil {
			spec.commit = *commit
		}
		id, err = proposeChecker(ctx, tx, t, d, spec)
		return err
	})
	return id, err
}

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

// adopt accepts the salvaged attempt: the subject's live attempt (locked
// before the subject) is cancelled, and the subject is accepted.
func adopt(ctx context.Context, tx pgx.Tx, s taskRow, accepted, live string) error {
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
	return moveTaskBy(ctx, tx, "controller", s, "accepted", "the salvaged result of attempt "+accepted+" passed review", adoptMoves)
}
