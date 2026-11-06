package store

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
)

// Closing the pending checks of a stopped task (11-06: two of M-308's three
// budget cards were for checks of tasks already stopped). A check is a
// verification or a review; ensureCheck and stillChecked (accept.go) use only
// the checks of a subject that is verifying, so one whose subject is blocked,
// cancelled or rejected can never decide anything: a subject given one more
// round runs again, and its new attempt gets checks of its own. The
// controller's pass cancels such a check while it is pending (proposed,
// awaiting_owner, ready, paused or blocked) before it can be blocked for
// budget or carded, and closes its open card as moot (the next
// WithdrawStaleCards withdraws it from the question store). A leased or
// running check is left to end on its own, and a gate task to its batch. A
// check cancelled this way does not hold its mission open (close.go).
//
// Locks, in the kernel's order (a subject before its check, as Conclude and
// DecideBlocked take them): the subject shared, so it cannot move to ready
// between the read and the cancel, then the check; both read again under
// them. The subject's own moves never cancel its checks: the cancel is this
// pass's alone.

// stoppedSubject begins the reason a check cancelled here records; close.go
// reads it to let the check's mission close.
const stoppedSubject = "its subject task "

// The states of a stopped subject and of a pending check, as SQL lists.
const (
	stoppedStates = `('blocked','cancelled','rejected')`
	pendingStates = `('proposed','awaiting_owner','ready','paused','blocked')`
)

// checkOfStopped is a check (t) whose subject is stopped.
const checkOfStopped = `t.role IN ('verify','review') AND EXISTS (SELECT 1 FROM agentrt.task s
    WHERE s.task_id = t.subject_task_id AND s.state IN ` + stoppedStates + `)`

// CloseStoppedChecks is one pass cancelling every pending check of a stopped
// task, page by page, each in a transaction of its own, so one that cannot be
// cancelled now does not hold up the others; the next pass tries it again. It
// returns how many it cancelled.
func CloseStoppedChecks(ctx context.Context, db DB) (int, error) {
	n := 0
	var errs []error
	after := ""
	page := int(blockedPage.Load())
	for {
		var ids []string
		if err := InTx(ctx, db, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `SELECT t.task_id FROM agentrt.task t WHERE t.task_id > $1 AND t.state IN `+pendingStates+`
			  AND `+checkOfStopped+` ORDER BY t.task_id LIMIT $2`, after, page)
			if err != nil {
				return err
			}
			ids, err = pgx.CollectRows(rows, pgx.RowTo[string])
			return err
		}); err != nil {
			return n, errors.Join(append(errs, err)...)
		}
		for _, id := range ids {
			did, err := closeStoppedCheck(ctx, db, id)
			if err != nil {
				errs = append(errs, fmt.Errorf("task %s: %w", id, err))
			} else if did {
				n++
			}
		}
		if len(ids) < page {
			return n, errors.Join(errs...)
		}
		after = ids[len(ids)-1]
	}
}

// closeStoppedCheck cancels one check, read again under its subject's lock
// and its own, and closes its open cards as moot.
func closeStoppedCheck(ctx context.Context, db DB, id string) (bool, error) {
	did := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		did = false
		var subject string
		if err := tx.QueryRow(ctx, `SELECT COALESCE(subject_task_id, '') FROM agentrt.task WHERE task_id = $1`, id).Scan(&subject); err != nil || subject == "" {
			return err
		}
		var sstate string
		if err := tx.QueryRow(ctx, `SELECT state FROM agentrt.task WHERE task_id = $1 FOR SHARE`, subject).Scan(&sstate); err != nil {
			return err
		}
		t, err := readTask(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if !slices.Contains([]string{"verify", "review"}, t.role) || !slices.Contains([]string{"blocked", "cancelled", "rejected"}, sstate) ||
			!slices.Contains([]string{"proposed", "awaiting_owner", "ready", "paused", "blocked"}, t.state) {
			return nil
		}
		// A gate task is ended by its batch, not cancelled (gateend.go).
		if member, err := isGateMember(ctx, tx, id); err != nil || member {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT o.body_json->>'question_id' FROM agentrt.event o WHERE o.task_id = $1
		  AND o.event_type = 'owner_wait_opened' AND `+cardOpen+` ORDER BY o.event_id`, id)
		if err != nil {
			return err
		}
		cards, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		did = true
		if err = moveTask(ctx, tx, "controller", t, "cancelled",
			fmt.Sprintf("%s%s is %s; this check can no longer decide anything", stoppedSubject, subject, sstate)); err != nil {
			return err
		}
		for _, q := range cards {
			if _, err = Append(ctx, tx, Event{Stream: MissionStream(t.mission), Type: "owner_wait_closed", Actor: "controller",
				MissionID: t.mission, PlanNode: t.step, TaskID: t.id, Body: map[string]any{"question_id": q, "outcome": OutcomeMoot}}); err != nil {
				return err
			}
		}
		return nil
	})
	return did && err == nil, err
}
