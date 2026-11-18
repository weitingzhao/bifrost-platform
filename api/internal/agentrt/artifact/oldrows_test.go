package artifact_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

// oldShapeRow writes an event the way rows were written before the
// vocabulary narrowed (TD-583: note and policy_alert took fields and rules
// other domains wrote; each has its own type now): straight into
// agentrt.event, chained to its stream's head as the database requires, its
// sha256 the hash of its body and the head's. The current vocabulary is not
// asked, so the test can hold a reader to an old row; the stream verifier is
// not asked of these streams.
func oldShapeRow(ctx context.Context, tx pgx.Tx, stream, typ, actor, planNode string, body map[string]any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO agentrt.event_stream (event_stream_id) VALUES ($1) ON CONFLICT DO NOTHING`, stream); err != nil {
		return err
	}
	var seq int64
	var prev string
	if err = tx.QueryRow(ctx, `SELECT last_seq, last_sha256 FROM agentrt.event_stream WHERE event_stream_id = $1 FOR UPDATE`, stream).Scan(&seq, &prev); err != nil {
		return err
	}
	sum := sha256.Sum256(append([]byte(prev), raw...))
	_, err = tx.Exec(ctx, `INSERT INTO agentrt.event (event_stream_id, seq, event_type, actor_id, plan_node_id, body_json, prev_sha256, sha256, at)
	  VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6::jsonb, $7, $8, clock_timestamp())`,
		stream, seq+1, typ, actor, planNode, string(raw), prev, hex.EncodeToString(sum[:]))
	return err
}
