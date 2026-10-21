package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

// What a check is told of the attempt it checks (TD-409; STG M-93,
// 2026-10-20: an implementation of effect none slept its 2700 s and
// reported "slept 2700 s"; two cursor reviews looked for that line in an
// empty checkout and failed it twice, and the task blocked). A review was
// given the subject's spec and its deterministic check's verify.json, never
// what the attempt handed in, so the work of a task that changes no files
// could not be seen at all. Now the claim of a check names the report the
// checked attempt handed in (the attempt its spec pins: a salvaged one's
// too) and its task's effect class; agentd reads the report with the
// artifact route and puts it in a review's prompt after the subject's spec
// (agentd executors.go). The report was masked of local paths when agentd
// handed it in, and the platform takes no report that still has one (H8).

// subjectOf is the report the attempt a check's spec pins handed in ("" for
// none) and its task's effect class. The spec is read and parsed here, not
// cast in SQL: a statement that fails would end the claim's transaction, and
// a spec that names no attempt is only a check told of nothing.
func subjectOf(ctx context.Context, tx pgx.Tx, specID string) (report, effect string, err error) {
	var body []byte
	err = tx.QueryRow(ctx, `SELECT body FROM agentrt.artifact_blob WHERE artifact_id = $1`, specID).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	var spec struct {
		Attempt string `json:"attempt"`
	}
	if json.Unmarshal(body, &spec) != nil || spec.Attempt == "" {
		return "", "", nil
	}
	err = tx.QueryRow(ctx, `SELECT COALESCE(a.report_artifact_id, ''), s.effect_class FROM agentrt.attempt a
	  JOIN agentrt.task s ON s.task_id = a.task_id WHERE a.attempt_id = $1`, spec.Attempt).Scan(&report, &effect)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil
	}
	return report, effect, err
}
