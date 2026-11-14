package store

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jackc/pgx/v5"
)

// The opt-in record has exactly the item kind and id, not caller-supplied
// assessment content or state. The note vocabulary checks this before Append.
func badAssessmentRequest(v any) string {
	m, ok := v.(map[string]any)
	if !ok || len(m) != 2 {
		return "needs exactly kind and id"
	}
	kind, _ := m["kind"].(string)
	id, _ := m["id"].(string)
	if (kind != AssessCard && kind != AssessApproval) || strings.TrimSpace(id) == "" {
		return "needs a card or approval kind and a non-empty string id"
	}
	return ""
}

// ReadAssessment reads only the lasting record, including pre-opt-in results.
// Pages include this in their ordinary item read; opening a page does no work.
func ReadAssessment(ctx context.Context, db DB, kind, id string) (json.RawMessage, error) {
	var raw json.RawMessage
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object(
		  'event_type', event_type, 'body_json', body_json, 'at', at) ORDER BY seq), '[]')
		  FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'`, AssessStream(kind, id)).Scan(&raw)
	})
	return raw, err
}
