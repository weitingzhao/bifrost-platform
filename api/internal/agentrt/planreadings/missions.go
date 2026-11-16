package planreadings

import (
	"context"
	"github.com/jackc/pgx/v5"
	"time"
)

// WaitingMission observes a historical mission awaiting an Owner answer.
// It does not determine whether its question is still open or may be answered.
type WaitingMission struct {
	Mission, Title, Question, Why string
	At                            time.Time
	Open                          int
}

// WaitingMissions reads in database order with one extra row for truncation.
// Projection handles card deduplication; choices and permissions remain with the caller.
func WaitingMissions(ctx context.Context, tx pgx.Tx, limit int) ([]WaitingMission, error) {
	rows, err := tx.Query(ctx, `SELECT m.mission_id, m.title, s.at, s.body_json->>'question_id', COALESCE(s.body_json->>'reason', ''),
	    0
	  FROM agentrt.mission m
	  JOIN LATERAL (SELECT e.at, e.body_json FROM agentrt.event e WHERE e.mission_id = m.mission_id
	    AND e.event_type = 'mission_state_changed' ORDER BY e.event_id DESC LIMIT 1) s ON true
	  WHERE m.state = 'awaiting_owner' AND s.body_json ? 'question_id' ORDER BY s.at, m.mission_id LIMIT $1`, limit+1)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (WaitingMission, error) {
		var c WaitingMission
		return c, r.Scan(&c.Mission, &c.Title, &c.At, &c.Question, &c.Why, &c.Open)
	})
}
