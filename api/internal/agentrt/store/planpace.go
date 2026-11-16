package store

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planpace"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/workregistration"
	"time"
)

// Compatibility names keep the plan API unchanged while its computation
// belongs to the plan domain. This file only reads the observations.
type Pace = planpace.Pace
type PaceDay = planpace.PaceDay
type PaceETA = planpace.PaceETA
type PaceQuiet = planpace.PaceQuiet

const (
	PaceDays    = planpace.PaceDays
	PaceWindow  = planpace.PaceWindow
	QuietWindow = planpace.QuietWindow
	QuietMin    = planpace.QuietMin
)

// PlanPace reads a round's pace at now: the steps under root still planned
// or active, and its plan stream's moves into and out of done in the last
// PaceDays local days. A root there is not is pgx.ErrNoRows.
func PlanPace(ctx context.Context, db DB, root string, now time.Time) (Pace, error) {
	loc, lerr := time.LoadLocation(BriefZone)
	if lerr != nil {
		return Pace{}, lerr
	}
	var out Pace
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		var kind string
		if err := tx.QueryRow(ctx, `SELECT node_kind FROM agentrt.plan_node WHERE plan_node_id = $1 AND parent_plan_node_id IS NULL`, root).Scan(&kind); err != nil {
			return err
		}
		var remaining int
		if err := tx.QueryRow(ctx, viewTree+`SELECT count(*) FROM tree JOIN agentrt.plan_node n USING(plan_node_id)
		  WHERE n.node_kind = 'step' AND n.state IN ('planned','active')`, root).Scan(&remaining); err != nil {
			return err
		}
		start := planpace.LocalMidnight(now.In(loc)).AddDate(0, 0, -(PaceDays - 1))
		rows, err := tx.Query(ctx, `SELECT at, body_json->>'from', body_json->>'to' FROM agentrt.event
		  WHERE event_stream_id = $1 AND event_type = 'plan_node_state_changed' AND at >= $2
		    AND (body_json->>'to' = 'done' OR body_json->>'from' = 'done')`, PlanStream(root), start)
		if err != nil {
			return err
		}
		var moves []planpace.Move
		for rows.Next() {
			var m planpace.Move
			if err = rows.Scan(&m.At, &m.From, &m.To); err != nil {
				rows.Close()
				return err
			}
			moves = append(moves, m)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		out = planpace.Compute(root, remaining, moves, now, loc)
		spans, err := workregistration.WorkSpans(ctx, tx, now.Add(-QuietWindow), now)
		if err != nil {
			return err
		}
		out.Quiet, out.QuietMinutes, out.QuietSummary = planpace.Quiet(spans, now.Add(-QuietWindow), now, loc)
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Pace{}, fmt.Errorf("store: no plan %s: %w", root, err)
	}
	return out, err
}
