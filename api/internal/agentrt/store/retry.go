package store

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// Retrying after a failure (TD-381, TD-382; v5 §5.1.2 「分流顺序」, §6.2).
//
// An attempt its executor failed (it ended on its own or at the hard limit
// without a result the platform took) sends its task back to ready, where the
// next claim used to lease it at once, to the same executor if it was the
// only candidate: on STG a cursor that could not start spent a mission's
// eight attempts in 36 seconds (TD-381). Now the executor that failed waits
// before it is offered the task again, longer after each failure in a row,
// and after failuresToStop failures in a row it is not offered the task
// again; the controller records a policy_alert when that happens. Another
// candidate may take the task at once, as the route allows (the cascade).
// An attempt that ended any other way (a stop, a restart of agentd or of the
// host, a lost lease, capacity, or a result taken) does not count and ends
// the run: a run is what the task's latest attempts failed in a row.

const (
	// retryBackoff is how long an executor waits after its first failure
	// in a row; each further failure doubles it.
	retryBackoff = 30 * time.Second
	// failuresToStop is the number of failures in a row after which an
	// executor is no longer offered the task.
	failuresToStop = 3
)

// executorEnds are the ways an attempt ends that are its executor's doing.
var executorEnds = []string{"exit", "hard_limit"}

// failureRun is a task's latest failures in a row, by executor: how many,
// and when the latest ended; now is the database clock when it was read.
type failureRun struct {
	now time.Time
	by  map[string]runOf
}

type runOf struct {
	n    int
	last time.Time
}

// readFailureRun reads the task's attempts newest first while each failed
// by its executor's doing.
func readFailureRun(ctx context.Context, tx pgx.Tx, taskID string) (failureRun, error) {
	run := failureRun{by: map[string]runOf{}}
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&run.now); err != nil {
		return run, err
	}
	rows, err := tx.Query(ctx, `SELECT executor_actor_id, state, COALESCE(end_how, ''), ended_at FROM agentrt.attempt
	  WHERE task_id = $1 ORDER BY attempt_no DESC`, taskID)
	if err != nil {
		return run, err
	}
	defer rows.Close()
	for rows.Next() {
		var executor, state, endHow string
		var ended *time.Time
		if err = rows.Scan(&executor, &state, &endHow, &ended); err != nil {
			return run, err
		}
		if state != "failed" || ended == nil || !slices.Contains(executorEnds, endHow) {
			break
		}
		r := run.by[executor]
		if r.n == 0 {
			r.last = *ended
		}
		r.n++
		run.by[executor] = r
	}
	return run, rows.Err()
}

// backoff is how long an executor waits after n failures in a row.
func backoff(n int) time.Duration {
	if n <= 0 {
		return 0
	}
	return retryBackoff << (n - 1)
}

// holdsBack says why the run keeps executor from the task now, or "".
func (run failureRun) holdsBack(executor string) string {
	r := run.by[executor]
	switch {
	case r.n >= failuresToStop:
		return "failed the task " + strconv.Itoa(r.n) + " times in a row"
	case r.n > 0 && run.now.Before(r.last.Add(backoff(r.n))):
		return "waiting after " + strconv.Itoa(r.n) + " failures in a row"
	}
	return ""
}

// Policy alert phrases of this file: the closed phrases the event records.
const (
	repeatedFailurePhrase = "failed three times in a row; not offered the task again"
	localPathPhrase       = "a local path (H8)"
	workspaceRefPhrase    = "another task's workspace"
)

// alertRepeatedFailure records a policy_alert on a's mission when the
// failure just recorded is executor's failuresToStop-th in a row on the
// task: from then on it is not offered the task (claim.go). It is recorded
// once, when the run reaches the count. a and its task must be locked.
func alertRepeatedFailure(ctx context.Context, tx pgx.Tx, a attemptRow, executor string) error {
	run, err := readFailureRun(ctx, tx, a.task)
	if err != nil || run.by[executor].n != failuresToStop {
		return err
	}
	return attemptEvent(ctx, tx, "controller", a, "policy_alert",
		map[string]any{"rule": "repeated_failure", "source": "attempt", "phrase": repeatedFailurePhrase})
}

// refusedRules are the rules by which the platform refuses an artifact:
// the phrase each records.
var refusedRules = map[string]string{"local_path": localPathPhrase, "workspace_ref": workspaceRefPhrase}

// RefusedReport records that the platform refused a report agentd uploaded
// (PUT /artifacts answered 422 by rule H8 or the workspace rule, TD-382):
// a policy_alert naming the rule, on the attempt that is handing over on the
// host now. An upload does not name its attempt, so the attempt is the one of
// the host that is live, computed and not yet persisted, within its lease's
// grace: agentd uploads the report between computed and persist. When the
// host has none or more than one such attempt, nothing is recorded (the
// attempt still ends with the reason that its result was not persisted).
// It returns the attempt the refusal is recorded on (a refusal of the same
// body on the same attempt is recorded once), or "".
func RefusedReport(ctx context.Context, db DB, hostID, kind, artifactID, rule string) (string, error) {
	phrase, known := refusedRules[rule]
	if !reportKinds[kind] || !known || hostID == "" {
		return "", nil
	}
	var on string
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT attempt_id FROM agentrt.attempt WHERE host_id = $1 AND state IN ('leased','running','stale')
		  AND computed_at IS NOT NULL AND persisted_at IS NULL
		  AND clock_timestamp() < lease_expires_at + make_interval(secs => $2) LIMIT 2`, hostID, Grace.Seconds())
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil || len(ids) != 1 {
			return err
		}
		a, err := lockAttempt(ctx, tx, ids[0], false)
		if err != nil {
			if errors.Is(err, ErrFenced) {
				return nil
			}
			return err
		}
		if a.host != hostID || !liveState(a.state) || !a.computed || a.persisted || !a.now.Before(a.expires.Add(Grace)) {
			return nil
		}
		var seen bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE attempt_id = $1 AND event_type = 'policy_alert'
		  AND body_json->>'artifact_id' = $2)`, a.id, artifactID).Scan(&seen); err != nil {
			return err
		}
		on = a.id
		if seen {
			return nil
		}
		return attemptEvent(ctx, tx, "controller", a, "policy_alert",
			map[string]any{"rule": rule, "source": kind, "phrase": phrase, "artifact_id": artifactID})
	})
	if err != nil {
		return "", err
	}
	return on, nil
}

// handOverFailure is why an attempt that ends live without a persisted
// result failed to hand it over, or "" when it never computed one: the rule
// that refused its report, when one was recorded on it.
func handOverFailure(ctx context.Context, tx pgx.Tx, a attemptRow) (string, error) {
	if !a.computed || a.persisted {
		return "", nil
	}
	var rule string
	err := tx.QueryRow(ctx, `SELECT body_json->>'rule' FROM agentrt.event WHERE attempt_id = $1 AND event_type = 'policy_alert'
	  AND body_json->>'rule' IN ('local_path', 'workspace_ref') ORDER BY event_id DESC LIMIT 1`, a.id).Scan(&rule)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "its result was computed but not persisted", nil
	case err != nil:
		return "", err
	}
	return "its report was refused by rule " + rule, nil
}
