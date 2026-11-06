package store

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Withdrawing proposed work (POST /tasks/withdraw): the actor that proposed
// a task, or its mission, cancels it before any of it ran. A stop is a hold
// only the Owner lifts, not a cancel (PROD 2026-11-06: the coordinator
// stopped tasks whose specs were wrong, they stayed ready with their cards
// open, and the Owner answered cards for abandoned work). Work that started
// is stopped by a control or dropped on the Owner's card, never withdrawn.
// The cards of the tasks withdrawn are the card pass's: nothing here touches
// a card.

// withdrawableTasks and withdrawableMissions are the states a task and its
// mission may be in for the task to be withdrawn.
var (
	withdrawableTasks    = []string{"proposed", "awaiting_owner", "ready", "blocked", "paused"}
	withdrawableMissions = []string{"proposed", "awaiting_owner", "active", "paused"}
)

// withdrawReasonRunes bounds the reason each task_state_changed records.
const withdrawReasonRunes = 500

// WithdrawTasks cancels the given tasks of the writer's proposing, with
// each unfinished check of them, in one transaction: all or none. The
// writer is the actor of a task's task_proposed or of its mission's
// mission_proposed; the controller and the Owner are not proposers here
// (ErrNotYours). A task that has had an attempt, is not waiting to run, is
// in a mission that has ended or is a batch of gates is ErrState, and so is
// one an unfinished task outside the list depends on: dispatch would block
// that one on a cancelled dependency. The idempotency key is the writer's:
// a retry gets the first answer, the same key with another request is
// ErrConflict. Each move is recorded as the writer's, its reason saying who
// withdrew it and why.
func WithdrawTasks(ctx context.Context, db DB, w Writer, key string, taskIDs []string, reason string) (map[string]any, error) {
	if key == "" {
		return nil, invalid("an idempotency key is required")
	}
	if strings.TrimSpace(reason) == "" {
		return nil, invalid("a withdrawal says why")
	}
	if len(taskIDs) == 0 || len(taskIDs) > MaxBatch {
		return nil, invalid("a withdrawal names 1 to %d tasks", MaxBatch)
	}
	ids := slices.Clone(taskIDs)
	slices.Sort(ids)
	for i, id := range ids {
		switch {
		case id == "":
			return nil, invalid("a task id is empty")
		case i > 0 && ids[i-1] == id:
			return nil, invalid("task %s is named twice", id)
		}
	}
	req := map[string]any{"task_ids": ids, "reason": reason}
	for try := 1; ; try++ {
		out, err := idempotentAs(ctx, db, w, "tasks/withdraw", key, req, nil, func(tx pgx.Tx) (map[string]any, error) {
			if err := ensureWriter(ctx, tx, w, false); err != nil {
				return nil, err
			}
			return withdrawTx(ctx, tx, w, ids, reason)
		})
		if !lockNotAvailable(err) {
			return out, err
		}
		if try == busyTries {
			return nil, fmt.Errorf("%w: withdrawing %s: %v", ErrBusy, strings.Join(ids, ", "), err)
		}
		time.Sleep(time.Duration(try*try) * 20 * time.Millisecond)
	}
}

// withdrawTx is WithdrawTasks's write; ids are sorted and distinct. Its
// locks are the kernel's order (closeOnCardOnce): the tasks and their
// unfinished checks, NOWAIT, in id order, then their missions.
func withdrawTx(ctx context.Context, tx pgx.Tx, w Writer, ids []string, reason string) (map[string]any, error) {
	rows, err := tx.Query(ctx, `SELECT task_id, mission_id, COALESCE(plan_node_id, ''), state, effect_class, role, task_class,
	    COALESCE(subject_task_id, '')
	  FROM agentrt.task WHERE task_id = ANY($1) OR (subject_task_id = ANY($1) AND state NOT IN ('accepted','rejected','cancelled'))
	  ORDER BY task_id FOR NO KEY UPDATE NOWAIT`, ids)
	if err != nil {
		return nil, err
	}
	tasks := map[string]taskRow{}
	checks := map[string][]taskRow{}
	err = func() error {
		defer rows.Close()
		for rows.Next() {
			var t taskRow
			var subject string
			if err := rows.Scan(&t.id, &t.mission, &t.step, &t.state, &t.effect, &t.role, &t.class, &subject); err != nil {
				return err
			}
			if slices.Contains(ids, t.id) {
				tasks[t.id] = t
			} else {
				checks[subject] = append(checks[subject], t)
			}
		}
		return rows.Err()
	}()
	if err != nil {
		return nil, err
	}
	var missing []string
	missions := map[string]bool{}
	for _, id := range ids {
		t, found := tasks[id]
		if !found {
			missing = append(missing, id)
			continue
		}
		missions[t.mission] = true
	}
	if len(missing) > 0 {
		return nil, invalid("there is no task %s", strings.Join(missing, ", "))
	}
	hook("withdraw: tasks locked", ids[0])
	type missionRow struct {
		state string
		gates bool
	}
	mrows, err := tx.Query(ctx, `SELECT mission_id, state, scope_json ? 'gate_run' FROM agentrt.mission
	  WHERE mission_id = ANY($1) ORDER BY mission_id FOR NO KEY UPDATE`, slices.Sorted(maps.Keys(missions)))
	if err != nil {
		return nil, err
	}
	ms := map[string]missionRow{}
	err = func() error {
		defer mrows.Close()
		for mrows.Next() {
			var id string
			var m missionRow
			if err := mrows.Scan(&id, &m.state, &m.gates); err != nil {
				return err
			}
			ms[id] = m
		}
		return mrows.Err()
	}()
	if err != nil {
		return nil, err
	}

	var started []string
	if err := pgxCollect(ctx, tx, &started, `SELECT DISTINCT task_id FROM agentrt.attempt WHERE task_id = ANY($1) ORDER BY task_id`, ids); err != nil {
		return nil, err
	}
	for _, id := range ids {
		t, m := tasks[id], ms[tasks[id].mission]
		switch {
		case slices.Contains(started, id):
			return nil, fmt.Errorf("%w: task %s has had an attempt; work that started is stopped by a control or dropped on the Owner's card, not withdrawn",
				ErrState, id)
		case !slices.Contains(withdrawableTasks, t.state):
			return nil, fmt.Errorf("%w: task %s is %s; only a task that is %s may be withdrawn",
				ErrState, id, t.state, strings.Join(withdrawableTasks, ", "))
		case !slices.Contains(withdrawableMissions, m.state):
			return nil, fmt.Errorf("%w: task %s is in mission %s, which is %s", ErrState, id, t.mission, m.state)
		case m.gates:
			return nil, fmt.Errorf("%w: task %s is in mission %s, a batch of gates, which is the controller's", ErrState, id, t.mission)
		}
	}

	for _, id := range ids {
		var taskBy, missionBy string
		if err := tx.QueryRow(ctx, `SELECT
		    COALESCE((SELECT actor_id FROM agentrt.event WHERE task_id = $1 AND event_type = 'task_proposed' ORDER BY event_id LIMIT 1), ''),
		    COALESCE((SELECT actor_id FROM agentrt.event WHERE mission_id = $2 AND event_type = 'mission_proposed' ORDER BY event_id LIMIT 1), '')`,
			id, tasks[id].mission).Scan(&taskBy, &missionBy); err != nil {
			return nil, err
		}
		if k := kindOf(w.Actor); k == "owner" || k == "controller" || (w.Actor != taskBy && w.Actor != missionBy) {
			return nil, fmt.Errorf("%w: %s did not propose task %s or its mission %s; only its proposer withdraws it",
				ErrNotYours, w.Actor, id, tasks[id].mission)
		}
	}

	withdrawn := slices.Clone(ids)
	for _, cs := range checks {
		for _, c := range cs {
			withdrawn = append(withdrawn, c.id)
		}
	}
	var dependents []string
	if err := pgxCollect(ctx, tx, &dependents, `SELECT DISTINCT d.task_id FROM agentrt.task_dependency d JOIN agentrt.task t ON t.task_id = d.task_id
	  WHERE d.depends_on_task_id = ANY($1) AND NOT d.task_id = ANY($2) AND t.state NOT IN ('accepted','rejected','cancelled')
	  ORDER BY d.task_id`, ids, withdrawn); err != nil {
		return nil, err
	}
	if len(dependents) > 0 {
		return nil, fmt.Errorf("%w: unfinished task %s depends on what would be withdrawn; withdraw it in the same list or not at all",
			ErrState, strings.Join(dependents, ", "))
	}

	why := oneLineRunes("withdrawn by "+w.Actor+": "+reason, withdrawReasonRunes)
	cancelled := []string{}
	for _, id := range ids {
		for _, c := range checks[id] {
			if err := moveTaskBy(ctx, tx, w.Actor, c, "cancelled", why, controllerMoves); err != nil {
				return nil, err
			}
			cancelled = append(cancelled, c.id)
		}
		if err := moveTaskBy(ctx, tx, w.Actor, tasks[id], "cancelled", why, controllerMoves); err != nil {
			return nil, err
		}
		hook("withdrawn", id)
	}
	slices.Sort(cancelled)
	return map[string]any{"withdrawn": ids, "checks_cancelled": cancelled, "by": w.Actor}, nil
}

// pgxCollect reads one text column of every row of q into out.
func pgxCollect(ctx context.Context, tx pgx.Tx, out *[]string, q string, args ...any) error {
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return err
	}
	*out, err = pgx.CollectRows(rows, pgx.RowTo[string])
	return err
}
