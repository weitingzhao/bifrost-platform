package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// A stopped task reads stopped, a withdrawn one withdrawn (the progress
// projection and the plan view). A stop control holds a task when it names
// the task, its mission or, for a verify or review task, its subject; only
// the Owner lifts it, so a stopped task waits on him, never as queued work.
// A task is withdrawn when the event that cancelled it is a task_state_changed
// written by its proposer's side (a thread:, seat: or agent: actor), never by
// the controller.

// ProgressStop is the stop control that holds a task.
type ProgressStop struct {
	ControlID string `json:"control_id"`
	By        string `json:"by"`
	Reason    string `json:"reason"`
	Since     string `json:"since"`
}

// stopWords is a stopped task's reason.
func stopWords(s *ProgressStop) string {
	return "stopped by " + s.By + ": " + s.Reason + " (control " + s.ControlID + "); only the Owner lifts it"
}

// stopHolding joins, as stop, the uncleared stop control that holds task t:
// one naming the task first, then its subject, then its mission, the oldest
// of each first.
const stopHolding = ` LEFT JOIN LATERAL (SELECT c.control_id, c.set_by_actor_id, c.reason, c.set_at FROM agentrt.control c
    WHERE c.control_kind = 'stop' AND c.cleared_at IS NULL
      AND ((c.scope = 'task' AND (c.scope_ref = t.task_id OR c.scope_ref = t.subject_task_id))
        OR (c.scope = 'mission' AND c.scope_ref = t.mission_id))
    ORDER BY c.scope_ref = t.task_id DESC, c.scope = 'task' DESC, c.set_at, c.control_id LIMIT 1) stop ON true `

// stopColumns are stopHolding's columns.
const stopColumns = `stop.control_id, stop.set_by_actor_id, stop.reason, stop.set_at`

// withdrawnSQL is, for a cancelled task t, the actor and reason of the event
// that cancelled it when that is a withdrawal, else nulls (the columns
// withdrawn_by, withdraw_reason, withdrawn_at).
const withdrawnSQL = ` LEFT JOIN LATERAL (SELECT e.actor_id AS withdrawn_by, COALESCE(e.body_json->>'reason', '') AS withdraw_reason,
    e.at AS withdrawn_at FROM (SELECT x.event_type, x.actor_id, x.body_json, x.at FROM agentrt.event x WHERE x.task_id = t.task_id
      AND x.event_type IN ('task_state_changed','task_cancelled') ORDER BY x.event_id DESC LIMIT 1) e
    WHERE t.state = 'cancelled' AND e.event_type = 'task_state_changed' AND e.body_json->>'to' = 'cancelled'
      AND (e.actor_id LIKE 'thread:%' OR e.actor_id LIKE 'seat:%' OR e.actor_id LIKE 'agent:%')) wd ON true `

// stopScan receives stopColumns.
type stopScan struct {
	id, by, reason *string
	at             *time.Time
}

func (s *stopScan) dest() []any { return []any{&s.id, &s.by, &s.reason, &s.at} }

// stop is the control scanned, nil when none holds the task.
func (s *stopScan) stop(format func(time.Time) string) *ProgressStop {
	if s.id == nil {
		return nil
	}
	return &ProgressStop{ControlID: *s.id, By: *s.by, Reason: *s.reason, Since: format(*s.at)}
}

// planViewHolds adds to the plan view's tasks the stop that holds an
// unfinished one (stopped) and how a cancelled one was withdrawn (withdrawn),
// read in the view's transaction.
func planViewHolds(ctx context.Context, tx pgx.Tx, root string, tasks []map[string]any) error {
	rows, err := tx.Query(ctx, viewTree+`SELECT t.task_id, t.state NOT IN ('accepted','rejected','cancelled'), `+stopColumns+`,
	    wd.withdrawn_by, wd.withdraw_reason, wd.withdrawn_at
	  FROM agentrt.task t JOIN tree USING(plan_node_id)`+stopHolding+withdrawnSQL, root)
	if err != nil {
		return err
	}
	defer rows.Close()
	byID := make(map[string]map[string]any, len(tasks))
	for _, t := range tasks {
		byID[t["task_id"].(string)] = t
	}
	for rows.Next() {
		var id string
		var open bool
		var s stopScan
		var by, reason *string
		var at *time.Time
		if err = rows.Scan(append(append([]any{&id, &open}, s.dest()...), &by, &reason, &at)...); err != nil {
			return err
		}
		t := byID[id]
		if t == nil {
			continue
		}
		if st := s.stop(timeText); st != nil && open {
			t["stopped"] = st
		}
		if by != nil {
			t["withdrawn"] = map[string]any{"by": *by, "reason": *reason, "at": timeText(*at)}
		}
	}
	return rows.Err()
}
