package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Attempts that failed before their executor started (TD-394; the Owner
// 2026-10-20 21:3x 「TD-394 B」).
//
// agentd ends an attempt whose setup, checkout or sibling clone failed
// prepare_failed, and hands in what failed as its last checkpoint (agentd
// prepare.go). Such an end is the host's or the repository's preparation,
// not the executor's: TD-381 and TD-395 count only exit and hard_limit
// (executorEnds), so it withdraws no executor (PROD M-22: codex, cursor and
// claude were each withdrawn three times for a setup that never let one of
// them start). Three in a row, of the attempts leased since the task last
// went back to ready from a block, block it in BlockWithdrawn's pass, with
// prepareBlockMark; its card says so and quotes the last one's report
// (lastPrepare), and one more round tries it three times more.

// prepareFailed is the end of an attempt that failed before its executor
// started.
const prepareFailed = "prepare_failed"

// prepareBlockMark starts the reason of a task blocked because its
// preparation failed (a card tells such a block by it).
const prepareBlockMark = "its preparation failed before any executor started: "

// preparesFailed is the reason to block task id when its last failuresToStop
// attempts since it last went back to ready from a block all ended
// prepare_failed, else "".
func preparesFailed(ctx context.Context, tx pgx.Tx, id string) (string, error) {
	rows, err := tx.Query(ctx, `SELECT a.attempt_no, a.state, COALESCE(a.end_how, '') FROM agentrt.attempt a
	  WHERE a.task_id = $1 AND a.attempt_no > COALESCE((SELECT max((l.body_json->>'attempt_no')::int) FROM agentrt.event l
	    WHERE l.task_id = $1 AND l.event_type = 'attempt_leased' AND l.event_id < (SELECT max(r.event_id) FROM agentrt.event r
	      WHERE r.task_id = $1 AND r.event_type = 'task_state_changed' AND r.body_json->>'from' = 'blocked' AND r.body_json->>'to' = 'ready')), 0)
	  ORDER BY a.attempt_no DESC LIMIT $2`, id, failuresToStop)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var nos []string
	for rows.Next() {
		var no int
		var state, end string
		if err = rows.Scan(&no, &state, &end); err != nil {
			return "", err
		}
		if state != "failed" || end != prepareFailed {
			return "", nil
		}
		nos = append([]string{strconv.Itoa(no)}, nos...)
	}
	if err = rows.Err(); err != nil || len(nos) < failuresToStop {
		return "", err
	}
	return prepareBlockMark + fmt.Sprintf("attempts %s in a row (its setup, its checkout or a sibling; the host's or the repository's, "+
		"not an executor's: no executor is withdrawn for it)", strings.Join(nos, ", ")), nil
}

// lastPrepare is what a card quotes of the task's last attempt that failed
// before its executor: the report it handed in as a checkpoint (the only
// report a checkpoint names; agentd prepare.go), or a line saying it handed
// in none (an agentd from before TD-394). Nil when no attempt ended so.
func lastPrepare(ctx context.Context, tx pgx.Tx, taskID string) (*checkSummary, error) {
	c := checkSummary{method: "preparation", verdict: "failed"}
	err := tx.QueryRow(ctx, `SELECT attempt_id, attempt_no FROM agentrt.attempt WHERE task_id = $1 AND end_how = $2
	  ORDER BY attempt_no DESC LIMIT 1`, taskID, prepareFailed).Scan(&c.attempt, &c.round)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var body []byte
	err = tx.QueryRow(ctx, `SELECT b.body FROM agentrt.event e CROSS JOIN LATERAL jsonb_array_elements_text(e.body_json->'artifacts') x(id)
	  JOIN agentrt.artifact r ON r.artifact_id = x.id AND r.artifact_kind = 'report' JOIN agentrt.artifact_blob b ON b.artifact_id = r.artifact_id
	  WHERE e.task_id = $1 AND e.attempt_id = $2 AND e.event_type = 'attempt_checkpointed' ORDER BY e.event_id DESC LIMIT 1`,
		taskID, c.attempt).Scan(&body)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		c.excerpt = checkExcerpt{lead: "No report of what failed was handed in (its upload or its checkpoint may have been refused, or the agentd is from before TD-394)."}
	case err != nil:
		return nil, err
	default:
		c.excerpt = checkExcerpt{lead: "Its report:", text: strings.TrimSpace(strings.ToValidUTF8(string(body), "?"))}
	}
	return &c, nil
}
