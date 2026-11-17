package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Historical retry phrases and current artifact-refusal recording.
// The dispatcher no longer retries, withdraws or reassigns executors.

// Policy alert phrases of this file: the closed phrases the event records.
const (
	repeatedFailurePhrase = "failed three times in a row; not offered the task again"
	localPathPhrase       = "a local path (H8)"
	workspaceRefPhrase    = "another task's workspace"
)

// reportKinds are the kinds a result's report may be stored as.
var reportKinds = map[string]bool{"report": true, "verify": true, "review": true}

// refusedRules are the rules by which the platform refuses an artifact:
// the phrase each records.
var refusedRules = map[string]string{"local_path": localPathPhrase, "workspace_ref": workspaceRefPhrase}

// RefusedReport records an artifact refusal once per body and host. It keeps
// the existing host stream and report vocabulary for artifact readers.
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
		if seen, err := eventlog.Has(ctx, tx, stream, "policy_alert", map[string]string{"artifact_id": artifactID}); err != nil || seen {
			return err
		}
		_, err := Append(ctx, tx, Event{Stream: stream, Type: "policy_alert", Actor: "controller",
			Body: map[string]any{"rule": rule, "source": kind, "phrase": phrase, "artifact_id": artifactID}})
		return err
	})
	return err == nil, err
}
