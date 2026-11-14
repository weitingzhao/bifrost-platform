package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// Closing a whole mission on a blocked task's card (the Owner 2026-10-20:
// cancelling a check blocked the work it checked and made another card, so
// clearing STG's 16 missions left over from its walks took some forty
// answers). Every blocked card has a third option, "Close mission M-n";
// choosing it, in one transaction:
//   - every task of the mission that has not ended (accepted, rejected or
//     cancelled) is cancelled, whatever its state, and a live attempt of one
//     is cancelled first (attempt_cancelled), as accepting a salvaged result
//     cancels the live attempt it replaces (adopt.go): its host's next call
//     is refused and agentd ends it. A cancelled task gets no card, so a
//     check cancelled here blocks nothing;
//   - the card closes as owner_wait_closed, outcome closed, with the answer
//     kept (question_seen);
//   - the mission ends as a goal's mission whose tasks have all ended does
//     (close.go): done when an implementation (or a brief) of it was
//     accepted, else stopped; a paused mission, which may only stop, is
//     stopped. The move names the card and the answer, the Owner's decision.
//
// Its other open cards are then withdrawn by the next WithdrawStaleCards,
// which withdraws every open blocked card of a mission that has ended,
// however it ended (review 420 C1: such a card held a place in the question
// store's three until it expired). The pass that reads them closed records
// how (withdrawn), or, if the Owner answered one first, that it moved
// nothing (moot): an answer on a mission that has ended moves nothing, so a
// second "close" is a no-op. Plan-change and mission cards are not blocked
// cards and are left as they are.
//
// WithdrawStaleCards also withdraws a blocked card asked before cards had
// the third option (two options, no recommendation; STG and PROD held some
// when this came): the pass after asks its block again with a card of its
// own kind, so every open blocked card can close its mission.

// The third option of a blocked card, and its key in the question store.
const (
	CardCloseKey  = "c"
	OutcomeClosed = "closed" // the Owner closed the task's mission on its card
)

// BlockedClose is the third option of a card of a task of mission.
func BlockedClose(mission string) string { return "Close mission " + mission }

// blockedOptions are a card's options, in the question store's order.
func blockedOptions(mission string) []string {
	return []string{BlockedRetry, BlockedDrop, BlockedClose(mission)}
}

// closeSays is what a card says the third option does.
func closeSays(mission string) string {
	return fmt.Sprintf("every task of mission %s that has not ended is cancelled (an attempt still running is stopped), "+
		"its other open cards are withdrawn, and it ends: done when an implementation of it was accepted, else stopped", mission)
}

// closeMoves are the task moves of closing a mission: every state that has
// not ended, to cancelled. Kept out of taskMoves, as adopt's are: nothing
// else cancels a task with a live attempt or a result under verification.
var closeMoves = func() map[string][]string {
	m := map[string][]string{}
	for _, s := range []string{"proposed", "awaiting_owner", "ready", "leased", "running", "submitted", "verifying", "rework", "blocked", "paused"} {
		m[s] = []string{"cancelled"}
	}
	return m
}()

// closeOnCard closes the mission of card c on the Owner's answer q; did is
// whether it closed the card. A close that finds a task's row locked, or the
// mission's tasks or live attempts changed between its reads, starts again,
// a few times; after that the next pass tries again.
func closeOnCard(ctx context.Context, db DB, c openCard, q Question, seen map[string]any, key string) (bool, error) {
	for try := 1; ; try++ {
		did, err := closeOnCardOnce(ctx, db, c, q, seen, key)
		if !errors.Is(err, errLiveMoved) && !retryable(err) {
			return did, err
		}
		if try == busyTries {
			return false, fmt.Errorf("%w: closing the mission of card %s: %v", ErrBusy, c.question, err)
		}
		time.Sleep(time.Duration(try*try) * 20 * time.Millisecond)
	}
}

// closeOnCardOnce closes the mission the card was asked in (the one its
// option names; owner_wait_opened records it), never the one its task is in
// now, if a plan change moved the task since (review 439 A4). Its locks, in
// the kernel's order: the live attempts of the mission's tasks; the tasks
// that have not ended, NOWAIT, since Dispatch takes a dependent before what
// it waits for and no fixed order fits every graph (review 439 A2: a row
// locked elsewhere starts the close again); then the mission. Reads after
// the mission's lock see what was committed before it: a live attempt or an
// unfinished task that is not among those locked starts the close again
// (review 439 A1). A task that lands in the mission after this commits (a
// proposal reads the mission without its lock) is cancelled by
// CancelAfterClose.
func closeOnCardOnce(ctx context.Context, db DB, c openCard, q Question, seen map[string]any, key string) (bool, error) {
	did := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		did = false
		var mission string
		if err := tx.QueryRow(ctx, `SELECT mission_id FROM agentrt.event WHERE event_id = $1`, c.event).Scan(&mission); err != nil {
			return err
		}
		attempts, err := liveOf(ctx, tx, mission, "FOR UPDATE OF a")
		if err != nil {
			return err
		}
		hook("close: attempts locked", mission)
		open, err := unfinishedOf(ctx, tx, mission, "FOR NO KEY UPDATE NOWAIT")
		if err != nil {
			return err
		}
		hook("close: tasks locked", mission)
		var mstate string
		if err = tx.QueryRow(ctx, `SELECT state FROM agentrt.mission WHERE mission_id = $1 FOR NO KEY UPDATE`, mission).Scan(&mstate); err != nil {
			return err
		}
		nowLive, err := liveOf(ctx, tx, mission, "")
		if err != nil {
			return err
		}
		nowOpen, err := unfinishedOf(ctx, tx, mission, "")
		if err != nil {
			return err
		}
		if !slices.Equal(nowLive, attempts) || !slices.Equal(nowOpen, open) {
			return errLiveMoved
		}
		var closed bool
		var latest int64
		if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE task_id = $1 AND event_type = 'owner_wait_closed'
		    AND body_json->>'question_id' = $2),
		  COALESCE((SELECT e.event_id FROM agentrt.event e WHERE e.task_id = $1 AND e.event_type = 'task_state_changed' ORDER BY e.event_id DESC LIMIT 1), 0)`,
			c.task, c.question).Scan(&closed, &latest); err != nil || closed {
			return err // another pass decided it
		}
		if _, err = recordIfChanged(ctx, tx, "question_seen", key, func(context.Context) (map[string]any, bool, error) {
			return seen, true, nil
		}); err != nil {
			return err
		}
		t, err := readTask(ctx, tx, c.task, false)
		if err != nil {
			return err
		}
		// Nothing moves when the mission has ended (a second close
		// included), the task is in another mission now (its next card names
		// that one), the task left the block the card is about (review
		// 439 C2, as one more round and not doing it go), or a stop holds the
		// task or the mission (blockedcard.go's stopHolds; both rows are
		// locked above).
		stopped, err := stopHolds(ctx, tx, t)
		if err != nil {
			return err
		}
		acted := OutcomeClosed
		if (mstate != "active" && mstate != "paused") || t.mission != mission || t.state != "blocked" || latest != c.block || stopped {
			acted = OutcomeMoot
		}
		by := answerOf(q)
		body := map[string]any{"question_id": c.question, "outcome": acted, "answer": q.Option}
		if q.Text != "" {
			body["text"] = q.Text
		}
		if by.rule != "" {
			body["by"] = by.rule
		}
		if _, err = Append(ctx, tx, Event{Stream: MissionStream(t.mission), Type: "owner_wait_closed", Actor: "controller",
			MissionID: t.mission, PlanNode: t.step, TaskID: t.id, Body: body}); err != nil {
			return err
		}
		if err = ensureRuleNote(ctx, tx, mission, t, by, c.question, q.Option); err != nil {
			return err
		}
		did = true
		if acted == OutcomeMoot {
			return nil
		}
		why := fmt.Sprintf("%s closed mission %s on the card of task %s (question %s)", by.who, mission, c.task, c.question)
		if err = cancelAll(ctx, tx, attempts, open, why); err != nil {
			return err
		}
		var accepted int
		var delivered bool
		if err = tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE state = 'accepted'),
		    COALESCE(bool_or(role IN ('implement','brief') AND state = 'accepted'), false)
		  FROM agentrt.task WHERE mission_id = $1`, mission).Scan(&accepted, &delivered); err != nil {
			return err
		}
		on := fmt.Sprintf("%s closed it on the card of task %s (question %s)", by.who, c.task, c.question)
		to, reason := "stopped", fmt.Sprintf("%s; %s cancelled with it, nothing the goal asked for was accepted", on, tasks(len(open)))
		switch {
		case delivered && mstate == "active":
			to, reason = "done", fmt.Sprintf("%s; %s accepted, %d cancelled with it", on, tasks(accepted), len(open))
		case delivered:
			reason = fmt.Sprintf("%s while it was paused; %s accepted, %d cancelled with it", on, tasks(accepted), len(open))
		}
		return moveMission(ctx, tx, "controller", mission, to, reason, c.question, q.Option)
	})
	return did && err == nil, err
}

// liveOf is the live attempts of a mission's tasks, with the given lock
// clause ("" for none).
func liveOf(ctx context.Context, tx pgx.Tx, mission, lock string) ([]attemptRow, error) {
	rows, err := tx.Query(ctx, `SELECT a.attempt_id, a.task_id FROM agentrt.attempt a JOIN agentrt.task t ON t.task_id = a.task_id
	  WHERE t.mission_id = $1 AND a.state IN ('leased','running','stale') ORDER BY a.attempt_id `+lock, mission)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (attemptRow, error) {
		var a attemptRow
		return a, r.Scan(&a.id, &a.task)
	})
}

// unfinishedOf is a mission's tasks that have not ended, the work before
// its checks, with the given lock clause ("" for none).
func unfinishedOf(ctx context.Context, tx pgx.Tx, mission, lock string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT task_id FROM agentrt.task WHERE mission_id = $1 AND state NOT IN ('accepted','rejected','cancelled')
	  ORDER BY subject_task_id IS NOT NULL, task_id `+lock, mission)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// cancelAll cancels the given live attempts (attempt_cancelled; their hosts'
// next calls are refused and agentd ends them) and then the given tasks, all
// locked by the caller, with why.
func cancelAll(ctx context.Context, tx pgx.Tx, attempts []attemptRow, ids []string, why string) error {
	for _, a := range attempts {
		if err := attemptEvent(ctx, tx, "controller", a, "attempt_cancelled", map[string]any{"reason": why}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE agentrt.attempt SET state = 'cancelled' WHERE attempt_id = $1`, a.id); err != nil {
			return err
		}
	}
	for _, id := range ids {
		o, err := readTask(ctx, tx, id, false)
		if err != nil {
			return err
		}
		if err = moveTaskBy(ctx, tx, "controller", o, "cancelled", why, closeMoves); err != nil {
			return err
		}
	}
	return nil
}

// tasks is n tasks, in words.
func tasks(n int) string {
	if n == 1 {
		return "1 task"
	}
	return fmt.Sprintf("%d tasks", n)
}
