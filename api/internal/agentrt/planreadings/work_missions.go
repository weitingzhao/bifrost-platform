package planreadings

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
	"time"
)

// WorkMission is the recorded mission and ordered step observation, without task execution or evidence decisions.
type WorkMission struct {
	ID, Title, Goal, State, Deadline, Work               string
	StateAt, StateReason, StateBy, StateQuestion, LastAt string
	Steps                                                []plan.Step
	Next                                                 []any
}

// WorkMissions reads both sets in the caller's unchanged transaction.
func WorkMissions(ctx context.Context, tx pgx.Tx, limit int) ([]*WorkMission, bool, error) {
	rows, err := tx.Query(ctx, `SELECT m.mission_id, m.Title, m.Goal, m.State, m.deadline_at, COALESCE(m.work_id, ''),
	    s.at, COALESCE(s.body_json->>'reason', ''), COALESCE(s.actor_id, ''), COALESCE(s.body_json->>'question_id', ''), l.at
	  FROM agentrt.mission m
	  LEFT JOIN LATERAL (SELECT e.at, e.body_json, e.actor_id FROM agentrt.event e WHERE e.mission_id = m.mission_id
	    AND e.event_type IN ('mission_proposed','mission_state_changed') ORDER BY e.event_id DESC LIMIT 1) s ON true
	  LEFT JOIN LATERAL (SELECT e.at FROM agentrt.event e WHERE e.mission_id = m.mission_id ORDER BY e.event_id DESC LIMIT 1) l ON true
	  WHERE m.State IN ('proposed','awaiting_owner','active','paused') ORDER BY substr(m.mission_id, 3)::int LIMIT $1`, limit+1)
	if err != nil {
		return nil, false, err
	}
	var missions []*WorkMission
	byID := map[string]*WorkMission{}
	for rows.Next() {
		m := &WorkMission{}
		var deadline time.Time
		var stateAt, lastAt *time.Time
		if err = rows.Scan(&m.ID, &m.Title, &m.Goal, &m.State, &deadline, &m.Work, &stateAt, &m.StateReason, &m.StateBy,
			&m.StateQuestion, &lastAt); err != nil {
			rows.Close()
			return nil, false, err
		}
		m.Deadline = deadline.UTC().Format(time.RFC3339)
		m.StateAt, m.LastAt = workStamp(stateAt), workStamp(lastAt)
		missions = append(missions, m)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	truncated := len(missions) > limit
	if truncated {
		missions = missions[:limit]
	}
	ids := make([]string, 0, len(missions))
	for _, m := range missions {
		byID[m.ID] = m
		ids = append(ids, m.ID)
	}
	// The slices: the steps of each mission, by the counting rule, and the
	// next steps not started, in plan order.
	srows, err := tx.Query(ctx, `SELECT s.mission_id, s.plan_node_id, s.title, s.state, COALESCE(e.body_json->>'cause', ''), s.owner_step
	  FROM agentrt.plan_node s JOIN agentrt.plan_node i ON i.plan_node_id = s.parent_plan_node_id
	  LEFT JOIN agentrt.event e ON e.event_id = s.state_event_id
	  WHERE s.node_kind = 'step' AND s.mission_id = ANY($1)
	  ORDER BY i.ordinal, s.ordinal, s.plan_node_id`, ids)
	if err != nil {
		return nil, false, err
	}
	for srows.Next() {
		var mission, id, title, state, cause string
		var owner bool
		if err = srows.Scan(&mission, &id, &title, &state, &cause, &owner); err != nil {
			srows.Close()
			return nil, false, err
		}
		m := byID[mission]
		m.Steps = append(m.Steps, plan.Step{ID: id, State: state, Manual: state == plan.StateDone && cause == "done_rule_manual"})
		if state == plan.StatePlanned && len(m.Next) < 2 {
			m.Next = append(m.Next, map[string]any{"plan_node_id": id, "title": title, "owner_step": owner})
		}
	}
	srows.Close()
	if err = srows.Err(); err != nil {
		return nil, false, err
	}
	return missions, truncated, nil
}
func workStamp(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
