package store

import (
	"context"

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
//
// Once every executor on a ready task's route is withdrawn, the controller
// blocks it and it gets the blocked card (withdrawn.go, TD-395). "Give it one
// more round" there offers the task once more to each executor withdrawn from
// it: one more failure withdraws it again, and the task is blocked again
// with a new card. The round starts at the task's move back to ready, whose
// reason carries liftMark; the failures leased after it are the ones since.

// Policy alert phrases of this file: the closed phrases the event records.
const (
	repeatedFailurePhrase = "failed three times in a row; not offered the task again"
	localPathPhrase       = "a local path (H8)"
	workspaceRefPhrase    = "another task's workspace"
)

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
