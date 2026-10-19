package store

import (
	"context"
	"errors"
	"fmt"
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
	// The clock and the newest attempts in one statement (review 400 C1),
	// at most runRows of them: a run is ended long before by the stop.
	rows, err := tx.Query(ctx, `SELECT c.now, a.executor_actor_id, a.state, COALESCE(a.end_how, ''), a.ended_at
	  FROM (SELECT clock_timestamp() AS now) c LEFT JOIN LATERAL (SELECT * FROM agentrt.attempt WHERE task_id = $1
	    ORDER BY attempt_no DESC LIMIT $2) a ON true`, taskID, runRows)
	if err != nil {
		return run, err
	}
	defer rows.Close()
	broken := false
	for rows.Next() {
		var executor, state, endHow *string
		var ended *time.Time
		if err = rows.Scan(&run.now, &executor, &state, &endHow, &ended); err != nil {
			return run, err
		}
		if broken || executor == nil || *state != "failed" || ended == nil || !slices.Contains(executorEnds, *endHow) {
			broken = true
			continue
		}
		r := run.by[*executor]
		if r.n == 0 {
			r.last = *ended
		}
		r.n++
		run.by[*executor] = r
	}
	return run, rows.Err()
}

// runRows bounds the attempts a run is read from.
const runRows = 64

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
// (PUT /artifacts answered 422 by rule H8 or the workspace rule, TD-382): a
// policy_alert on its host's stream naming the rule, the report's kind and
// the refused body's id, once per body and host. An upload does not name its
// attempt, and an upload of one attempt's report (a salvage after a lost
// lease) can come while another attempt of the host hands over, so the
// refusal is the host's and no attempt's (review 400 A1); an attempt that then
// ends without its result says it was refused meanwhile (handOverFailure).
// It reports whether the refusal is on record.
func RefusedReport(ctx context.Context, db DB, hostID, kind, artifactID, rule string) (bool, error) {
	phrase, known := refusedRules[rule]
	if !reportKinds[kind] || !known || hostID == "" {
		return false, nil
	}
	stream := "host:" + hostID
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		// Held to the commit: the same body refused twice at once is
		// recorded once.
		if err := lockStream(ctx, tx, stream); err != nil {
			return err
		}
		var seen bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'policy_alert'
		  AND body_json->>'artifact_id' = $2)`, stream, artifactID).Scan(&seen); err != nil || seen {
			return err
		}
		_, err := Append(ctx, tx, Event{Stream: stream, Type: "policy_alert", Actor: "controller",
			Body: map[string]any{"rule": rule, "source": kind, "phrase": phrase, "artifact_id": artifactID}})
		return err
	})
	return err == nil, err
}

// handOverFailure is why an attempt that ends live without a persisted
// result did not hand it over, or "" when it never computed one. It says
// so, and when its host had a report refused since the attempt computed, it
// says that too, as what happened on the host, not as the attempt's cause:
// the refusal does not name its attempt (RefusedReport).
func handOverFailure(ctx context.Context, tx pgx.Tx, a attemptRow) (string, error) {
	if !a.computed || a.persisted {
		return "", nil
	}
	why := "its result was computed but not persisted"
	var rule string
	var event int64
	err := tx.QueryRow(ctx, `SELECT e.body_json->>'rule', e.event_id FROM agentrt.attempt a JOIN agentrt.event e
	    ON e.event_stream_id = 'host:' || a.host_id AND e.event_type = 'policy_alert' AND e.at >= a.computed_at
	  WHERE a.attempt_id = $1 AND e.body_json->>'rule' IN ('local_path', 'workspace_ref')
	  ORDER BY e.event_id DESC LIMIT 1`, a.id).Scan(&rule, &event)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return why, nil
	case err != nil:
		return "", err
	}
	return fmt.Sprintf("%s; meanwhile its host had a report refused by rule %s (event %d)", why, rule, event), nil
}
