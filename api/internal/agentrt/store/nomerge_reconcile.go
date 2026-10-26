package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrNoMergeUncertain requires Owner reconciliation, never an automatic retry
// or a claim that the approved batch failed without changing anything.
var ErrNoMergeUncertain = errors.New("store: no_merge outcome unknown; Owner must check the missions and approval events")

func noMergeEvent(ctx context.Context, tx pgx.Tx, m NoMergeMission, r NoMergeRequest, approvalID string) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `SELECT event_id FROM agentrt.event
	  WHERE mission_id=$1 AND event_stream_id=$8 AND actor_id='controller' AND event_type='mission_amended'
	  AND body_json->>'approval_id'=$2 AND body_json->>'no_merge'=$3 AND body_json->>'reason'=$3
	  AND body_json->>'archive_manifest_sha256'=$4 AND body_json->>'env'=$5
	  AND body_json->>'expected_version'=$6 AND body_json->>'version'=$7 ORDER BY seq DESC LIMIT 1`,
		m.MissionID, approvalID, r.Reason, r.ArchiveManifestSHA256, r.Env,
		fmt.Sprint(m.ExpectedVersion), fmt.Sprint(m.ExpectedVersion+1), MissionStream(m.MissionID)).Scan(&id)
	return id, err
}

// A COMMIT transport error does not say whether PostgreSQL committed. Read
// back in a fresh transaction (the production pool replaces dead connections).
// Locks wait for the original transaction to finish and keep all observations
// stable until the whole batch has been checked. No write is retried here.
func reconcileNoMerge(ctx context.Context, db DB, missions []NoMergeMission, r NoMergeRequest, approvalID string, commitErr error) ([]NoMergeResult, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	uncertain := func(err error) ([]NoMergeResult, error) {
		return nil, fmt.Errorf("%w: commit: %v; readback: %v", ErrNoMergeUncertain, commitErr, err)
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return uncertain(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var out []NoMergeResult
	unchanged := 0
	for _, m := range missions {
		var state string
		var version int64
		var present, equal bool
		err := tx.QueryRow(ctx, `SELECT state, version, scope_json ? 'no_merge',
		  COALESCE(scope_json->'no_merge' = to_jsonb($2::text), false)
		  FROM agentrt.mission WHERE mission_id=$1 FOR NO KEY UPDATE`, m.MissionID, r.Reason).Scan(&state, &version, &present, &equal)
		if err != nil {
			return uncertain(fmt.Errorf("mission %s: %w", m.MissionID, err))
		}
		eventID, err := noMergeEvent(ctx, tx, m, r, approvalID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return uncertain(err)
		}
		switch {
		case state == "done" && equal && version == m.ExpectedVersion+1 && err == nil:
			out = append(out, NoMergeResult{MissionID: m.MissionID, Version: version, NoMerge: r.Reason, EventID: eventID, Duplicate: true})
		case state == "done" && version == m.ExpectedVersion && (!present || equal) && errors.Is(err, pgx.ErrNoRows):
			unchanged++
		}
	}
	// This transaction only reads and locks; rolling it back cannot undo the
	// evidence. If the connection cannot finish even this check, stay unknown.
	if err := tx.Rollback(ctx); err != nil {
		return uncertain(err)
	}
	if len(out) == len(missions) {
		return out, nil
	}
	if unchanged == len(missions) {
		return nil, fmt.Errorf("no_merge commit failed; readback confirmed all missions unchanged: %w", commitErr)
	}
	return uncertain(fmt.Errorf("batch evidence is mixed or mismatched (%d confirmed, %d unchanged, %d total)", len(out), unchanged, len(missions)))
}
