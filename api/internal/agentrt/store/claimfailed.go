package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// What a retried implementation is told of the rounds before it (TD-405).
//
// Each round of an implementation starts from the task's base commit with
// the same spec. The check that sent the last round back reached only the
// card and the reviewer, so on PROD M-23 codex handed in the same compile
// error three rounds running, and the Owner's "one more round" bought
// nothing. Now the claim of an implementation in a round after the first
// carries the evidence of the task's last deterministic check and of its
// last review, each when it failed: agentd reads them (verify.json, masked
// of local paths when agentd handed it in, and the review's report) and puts
// them after the spec in the executor's prompt. A failure the STG test
// switch injected (TestNeedFailFirstVerify) says nothing of the work and is
// left out.

// FailedCheck is a check an earlier round of a task failed: its method
// (deterministic or review), the number of the attempt it checked, and the
// artifact of its report (the verification's evidence).
type FailedCheck struct {
	Method    string
	AttemptNo int
	Evidence  string
}

// failedChecks are the checks the claim of attempt n of an implementation
// carries: of the attempts before it, the last deterministic check and the
// last review, each only when it failed, and none in the task's first round.
func failedChecks(ctx context.Context, tx pgx.Tx, task string, n int) ([]FailedCheck, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (v.method) v.method, v.verdict, a.attempt_no, v.evidence_artifact_id
	  FROM agentrt.verification v JOIN agentrt.attempt a ON a.attempt_id = v.attempt_id JOIN agentrt.task t ON t.task_id = v.task_id
	  WHERE v.task_id = $1 AND t.round_no > 1 AND a.attempt_no < $2 AND v.method IN ('deterministic', 'review')
	    AND v.check_ref IS DISTINCT FROM $3
	  ORDER BY v.method, v.created_at DESC, v.verification_id DESC`, task, n, TestNeedFailFirstVerify)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FailedCheck
	for rows.Next() {
		var f FailedCheck
		var verdict string
		if err = rows.Scan(&f.Method, &verdict, &f.AttemptNo, &f.Evidence); err != nil {
			return nil, err
		}
		if verdict == "fail" {
			out = append(out, f)
		}
	}
	return out, rows.Err()
}
