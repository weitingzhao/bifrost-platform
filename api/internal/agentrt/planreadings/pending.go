package planreadings

import (
	"context"
	"github.com/jackc/pgx/v5"
	"time"
)

// PendingChange is an observation, not a decision or permission to apply a plan.
type PendingChange struct {
	Event                    int64
	Stream, Reason, Question string
	At                       time.Time
	Changes                  string
}

// PendingChanges visits proposals in the original database order. Processing
// stays between scans so a decoding error precedes later row errors as before.
// The caller retains change decoding and the Owner-card presentation.
func PendingChanges(ctx context.Context, tx pgx.Tx, now time.Time, window time.Duration, limit int, visit func(PendingChange) error) error {
	rows, err := tx.Query(ctx, `SELECT p.event_id, p.event_stream_id, p.body_json->>'reason', p.at,
	    p.body_json->>'question_id', (p.body_json->'changes')::text
	  FROM agentrt.event p
	  WHERE p.event_type = 'plan_change_proposed' AND p.at > $1::timestamptz - make_interval(secs => $2) AND p.body_json ? 'question_id'
	    AND NOT EXISTS (SELECT 1 FROM agentrt.event d WHERE d.event_type = 'plan_change_decided' AND d.at >= p.at
	      AND d.event_stream_id = p.event_stream_id AND (d.body_json->>'proposal_event_id')::bigint = p.event_id)
	  ORDER BY p.at, p.event_id LIMIT $3`, now, window.Seconds(), limit+1)
	if err != nil {
		return err
	}
	for rows.Next() {
		var p PendingChange
		if err = rows.Scan(&p.Event, &p.Stream, &p.Reason, &p.At, &p.Question, &p.Changes); err != nil {
			rows.Close()
			return err
		}
		if err = visit(p); err != nil {
			rows.Close()
			return err
		}
	}
	rows.Close()
	return rows.Err()
}

// AffectedMissions reads the missions of the supplied nodes and explicit bindings.
// The caller determines which operations contribute nodes or bindings.
func AffectedMissions(ctx context.Context, tx pgx.Tx, nodes, bound []string) ([]string, error) {
	var missions []string
	err := tx.QueryRow(ctx, `WITH steps AS (SELECT plan_node_id, mission_id FROM agentrt.plan_node WHERE node_kind = 'step'
	      AND (plan_node_id = ANY($1) OR parent_plan_node_id = ANY($1)))
	  SELECT ARRAY(SELECT DISTINCT m FROM (SELECT mission_id FROM steps
	      UNION ALL SELECT unnest($2::text[])) x(m)
	      WHERE m IS NOT NULL ORDER BY 1)`, nodes, bound).Scan(&missions)
	return missions, err
}
