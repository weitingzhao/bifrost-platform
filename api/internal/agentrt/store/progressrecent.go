package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// recentView is the last ended tasks, by the event that ended them, and the
// last failed attempts, whatever came of their tasks (SPEC §2.6, T06). Both
// read the events newest first through the (event_type, at) index, and break
// ties of time by event id at every step, so the same events always give the
// same lists. A task withdrawn (progressstop.go) says by whom and why; a task
// whose change is on main says that instead (status finished, on_main and its
// reason), whatever its state.
func recentView(ctx context.Context, tx pgx.Tx) (map[string]any, error) {
	var tasks, failed []byte
	err := tx.QueryRow(ctx, `SELECT COALESCE(jsonb_agg((to_jsonb(r) - 'withdrawn' - 'on_main') || COALESCE(r.on_main, r.withdrawn, '{}'::jsonb)
	    ORDER BY r.ended_at DESC, r.ended_event_id DESC), '[]'::jsonb) FROM (
	    SELECT t.task_id, t.mission_id, t.title, t.role, t.state, e.at AS ended_at, e.event_id AS ended_event_id, a.conclusion,
	      a.report_artifact_id, CASE WHEN wd.withdrawn_by IS NOT NULL
	        THEN jsonb_build_object('withdrawn_by', wd.withdrawn_by, 'withdraw_reason', wd.withdraw_reason) END AS withdrawn,
	      CASE WHEN landed.commit IS NOT NULL THEN jsonb_build_object('status', '`+StatusFinished+`',
	        'on_main', jsonb_build_object('commit', landed.commit, 'via', COALESCE(NULLIF(landed.via, ''), 'on main')),
	        'reason', 'on main of ' || COALESCE(t.repo, '') || ' as ' || left(landed.commit, 12) || ' (' || COALESCE(NULLIF(landed.via, ''), 'on main') || ')') END AS on_main
	    FROM (SELECT DISTINCT ON (task_id) task_id, at, event_id FROM (
	        (SELECT task_id, at, event_id FROM agentrt.event WHERE event_type = 'task_state_changed'
	          AND body_json->>'to' IN ('accepted','rejected','cancelled') ORDER BY at DESC, event_id DESC LIMIT $1 * 2)
	        UNION ALL
	        (SELECT task_id, at, event_id FROM agentrt.event WHERE event_type = 'task_cancelled' ORDER BY at DESC, event_id DESC LIMIT $1 * 2)) x
	      ORDER BY task_id, at DESC, event_id DESC) e
	    JOIN agentrt.task t ON t.task_id = e.task_id AND t.state IN ('accepted','rejected','cancelled')
	    LEFT JOIN LATERAL (SELECT conclusion, report_artifact_id FROM agentrt.attempt WHERE task_id = t.task_id
	      ORDER BY attempt_no DESC LIMIT 1) a ON true`+withdrawnSQL+landedNote+`
	    ORDER BY e.at DESC, e.event_id DESC LIMIT $1) r`, progressRecent).Scan(&tasks)
	if err != nil {
		return nil, err
	}
	err = tx.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(r ORDER BY r.ended_at DESC, r.ended_event_id DESC), '[]'::jsonb) FROM (
	    SELECT a.attempt_id, a.task_id, t.mission_id, e.at AS ended_at, e.event_id AS ended_event_id, a.report_artifact_id
	    FROM agentrt.event e JOIN agentrt.attempt a ON a.attempt_id = e.attempt_id JOIN agentrt.task t ON t.task_id = a.task_id
	    WHERE e.event_type = 'attempt_computed' AND e.body_json->>'conclusion' = 'failed'
	    ORDER BY e.at DESC, e.event_id DESC LIMIT $1) r`, progressRecent).Scan(&failed)
	if err != nil {
		return nil, err
	}
	return map[string]any{"tasks": rawJSON(tasks), "failed_attempts": rawJSON(failed)}, nil
}
