package store

import (
	"context"
	"encoding/json"
	"github.com/weitingzhao/bifrost-platform/api/internal/assessmentreadings"
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
		var err error
		raw, err = assessmentreadings.Events(ctx, tx, AssessStream(kind, id))
		return err
	})
	return raw, err
}
