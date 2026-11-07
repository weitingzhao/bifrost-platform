package store

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
)

// Closing an implementation whose change is on main (PROD 2026-11-06: some
// thirty tasks whose change main held still read ready or verifying, and
// their checks and attempts went on). A landed note (RecordLandings,
// prune.go) says main holds the task's change; a task that has not ended
// (accepted, rejected or cancelled) and has one, sent back by a red landing
// CI after it landed for instance, is closed by this pass, in one
// transaction each:
//   - its live attempt is cancelled (attempt_cancelled, as cancelAll does):
//     its host's next call is refused and agentd ends it;
//   - its open checks (verify and review tasks of it) are cancelled, with a
//     live attempt of one, and a reason that begins with stoppedSubject, so
//     close.go counts them as closed checks (checkClosed) and
//     WithdrawStaleCards withdraws their cards;
//   - the task moves to accepted, with a reason naming main's commit and the
//     way it got there, read from the note. WithdrawStaleCards then withdraws
//     its open card, its task having ended.
//
// A stop on the task, or on its mission, does not keep it open: the change is
// on main, and the stop has nothing left to hold.
//
// Locks, in the kernel's order (as closeOnCardOnce takes them): the live
// attempts of the task and its open checks, then the task and its checks,
// NOWAIT, then nothing on the mission. Everything is read again under them;
// a task that ended meanwhile, or whose note is gone, is left alone, and one
// whose row is held, or whose attempts or checks changed between the reads,
// is tried again by the next pass.

// onMainMoves are the task moves of closing on main: every state that has not
// ended, to accepted. Kept out of taskMoves, as closeMoves and adopt's are:
// nothing else accepts a task from any state.
var onMainMoves = func() map[string][]string {
	m := map[string][]string{}
	for _, s := range []string{"proposed", "awaiting_owner", "ready", "leased", "running", "submitted", "verifying", "rework", "blocked", "paused"} {
		m[s] = []string{"accepted"}
	}
	return m
}()

// unfinishedLandedNote is an implementation (t) that has not ended and has a landed note.
const unfinishedLandedNote = `t.role = 'implement' AND t.state NOT IN ('accepted','rejected','cancelled')
  AND EXISTS (SELECT 1 FROM agentrt.event e WHERE e.task_id = t.task_id AND e.event_type = 'note' AND e.body_json ? 'landed')`

// CloseOnMain is one pass accepting every implementation that has not ended
// and whose change a landed note says is on main, page by page, each in a
// transaction of its own, so one that cannot be closed now does not hold up
// the others. It returns how many it closed.
func CloseOnMain(ctx context.Context, db DB) (int, error) {
	n := 0
	var errs []error
	after := ""
	page := int(blockedPage.Load())
	for {
		var ids []string
		if err := InTx(ctx, db, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `SELECT t.task_id FROM agentrt.task t WHERE t.task_id > $1 AND `+unfinishedLandedNote+`
			  ORDER BY t.task_id LIMIT $2`, after, page)
			if err != nil {
				return err
			}
			ids, err = pgx.CollectRows(rows, pgx.RowTo[string])
			return err
		}); err != nil {
			return n, errors.Join(append(errs, err)...)
		}
		for _, id := range ids {
			did, err := closeOnMain(ctx, db, id)
			switch {
			case errors.Is(err, errLiveMoved) || retryable(err):
				// Busy: the next pass tries it again.
			case err != nil:
				errs = append(errs, fmt.Errorf("task %s: %w", id, err))
			case did:
				n++
			}
		}
		if len(ids) < page {
			return n, errors.Join(errs...)
		}
		after = ids[len(ids)-1]
	}
}

// openChecksOf is the checks of a task that have not ended, with the given
// lock clause ("" for none).
func openChecksOf(ctx context.Context, tx pgx.Tx, task, lock string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT task_id FROM agentrt.task WHERE subject_task_id = $1 AND role IN ('verify','review')
	  AND state NOT IN ('accepted','rejected','cancelled') ORDER BY task_id `+lock, task)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// liveOfTasks is the live attempts of the given tasks, with the given lock
// clause ("" for none).
func liveOfTasks(ctx context.Context, tx pgx.Tx, ids []string, lock string) ([]attemptRow, error) {
	rows, err := tx.Query(ctx, `SELECT attempt_id, task_id FROM agentrt.attempt WHERE task_id = ANY($1)
	  AND state IN ('leased','running','stale') ORDER BY attempt_id `+lock, ids)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (attemptRow, error) {
		var a attemptRow
		return a, r.Scan(&a.id, &a.task)
	})
}

// closeOnMain accepts one implementation whose change is on main, read again
// under its locks, cancelling its live attempt and its open checks first.
func closeOnMain(ctx context.Context, db DB, id string) (bool, error) {
	did := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		did = false
		checks, err := openChecksOf(ctx, tx, id, "")
		if err != nil {
			return err
		}
		attempts, err := liveOfTasks(ctx, tx, append([]string{id}, checks...), "FOR UPDATE")
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `SELECT 1 FROM agentrt.task WHERE task_id = $1 FOR NO KEY UPDATE NOWAIT`, id); err != nil {
			return err
		}
		if _, err = openChecksOf(ctx, tx, id, "FOR NO KEY UPDATE NOWAIT"); err != nil {
			return err
		}
		nowChecks, err := openChecksOf(ctx, tx, id, "")
		if err != nil {
			return err
		}
		nowLive, err := liveOfTasks(ctx, tx, append([]string{id}, nowChecks...), "")
		if err != nil {
			return err
		}
		if !slices.Equal(nowChecks, checks) || !slices.Equal(nowLive, attempts) {
			return errLiveMoved
		}
		t, err := readTask(ctx, tx, id, false)
		if err != nil || t.role != "implement" || slices.Contains([]string{"accepted", "rejected", "cancelled"}, t.state) {
			return err
		}
		var repo, landed, via string
		err = tx.QueryRow(ctx, `SELECT COALESCE(t.repo, ''), e.body_json->>'landed', COALESCE(e.body_json->>'landed_via', '')
		  FROM agentrt.task t JOIN agentrt.event e ON e.task_id = t.task_id
		  WHERE t.task_id = $1 AND e.event_type = 'note' AND e.body_json ? 'landed' ORDER BY e.event_id DESC LIMIT 1`, id).Scan(&repo, &landed, &via)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if via == "" {
			via = "on main"
		}
		main12 := landed
		if len(main12) > 12 {
			main12 = main12[:12]
		}
		did = true
		why := fmt.Sprintf("its change is on main of %s as %s (%s)", repo, main12, via)
		for _, a := range attempts {
			if err = attemptEvent(ctx, tx, "controller", a, "attempt_cancelled", map[string]any{"reason": why}); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `UPDATE agentrt.attempt SET state = 'cancelled' WHERE attempt_id = $1`, a.id); err != nil {
				return err
			}
		}
		for _, c := range checks {
			o, err := readTask(ctx, tx, c, false)
			if err != nil {
				return err
			}
			if err = moveTaskBy(ctx, tx, "controller", o, "cancelled",
				fmt.Sprintf("%s%s has its change on main of %s as %s; this check can no longer decide anything", stoppedSubject, id, repo, main12),
				closeMoves); err != nil {
				return err
			}
		}
		return moveTaskBy(ctx, tx, "controller", t, "accepted", why, onMainMoves)
	})
	return did && err == nil, err
}
