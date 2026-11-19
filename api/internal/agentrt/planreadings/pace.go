// Package planreadings reads planning observations within its caller's transaction.
// It does not own transactions, mutations, authorization or the HTTP surface.
package planreadings

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planpace"
	"time"
)

const planTree = `WITH RECURSIVE tree AS (SELECT plan_node_id FROM agentrt.plan_node WHERE plan_node_id=$1 UNION ALL SELECT n.plan_node_id FROM agentrt.plan_node n JOIN tree t ON n.parent_plan_node_id=t.plan_node_id) `

// WorkSpans reads, within tx, each piece of registered work's span that
// overlaps [from, to]. The plan does not read registered work itself: lineage
// owns it (workregistration.WorkSpans) and the caller passes its reader in
// (law 28), so this package does not import lineage's.
type WorkSpans func(ctx context.Context, tx pgx.Tx, from, to time.Time) ([][2]time.Time, error)

// Pace reads the selected plan's remaining steps, completion events and registered
// activity without leaving tx. The caller supplies the established stream and zone,
// and the reader of registered work's spans; without one Pace refuses.
func Pace(ctx context.Context, tx pgx.Tx, root, stream string, now time.Time, loc *time.Location, spansOf WorkSpans) (planpace.Pace, error) {
	var out planpace.Pace
	if spansOf == nil {
		return out, errors.New("plan pace: no reader of registered work is bound")
	}
	var kind string
	if err := tx.QueryRow(ctx, `SELECT node_kind FROM agentrt.plan_node WHERE plan_node_id = $1 AND parent_plan_node_id IS NULL`, root).Scan(&kind); err != nil {
		return out, err
	}
	var remaining int
	if err := tx.QueryRow(ctx, planTree+`SELECT count(*) FROM tree JOIN agentrt.plan_node n USING(plan_node_id)
	  WHERE n.node_kind = 'step' AND n.state IN ('planned','active')`, root).Scan(&remaining); err != nil {
		return out, err
	}
	start := planpace.LocalMidnight(now.In(loc)).AddDate(0, 0, -(planpace.PaceDays - 1))
	rows, err := tx.Query(ctx, `SELECT at, body_json->>'from', body_json->>'to' FROM agentrt.event
	  WHERE event_stream_id = $1 AND event_type = 'plan_node_state_changed' AND at >= $2
	    AND (body_json->>'to' = 'done' OR body_json->>'from' = 'done')`, stream, start)
	if err != nil {
		return out, err
	}
	var moves []planpace.Move
	for rows.Next() {
		var m planpace.Move
		if err = rows.Scan(&m.At, &m.From, &m.To); err != nil {
			rows.Close()
			return out, err
		}
		moves = append(moves, m)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return out, err
	}
	out = planpace.Compute(root, remaining, moves, now, loc)
	spans, err := spansOf(ctx, tx, now.Add(-planpace.QuietWindow), now)
	if err != nil {
		return out, err
	}
	out.Quiet, out.QuietMinutes, out.QuietSummary = planpace.Quiet(spans, now.Add(-planpace.QuietWindow), now, loc)
	return out, nil
}
