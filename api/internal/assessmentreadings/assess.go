package assessmentreadings

import (
	"context"
	"encoding/json"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"strings"

	"github.com/jackc/pgx/v5"
)

// AI assessments are advice only; historical stream readers retain these names.
const (
	AssessCard     = "card"
	AssessApproval = "approval"
)

// AssessStream names the item's recorded assessment stream.
func AssessStream(kind, id string) string { return "assess:" + kind + ":" + id }

// The opt-in record has exactly the item kind and id, not caller-supplied
// assessment content or state. The note vocabulary (declared in agentrt/store)
// checks this before Append.
func BadAssessmentRequest(v any) string {
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
func ReadAssessment(ctx context.Context, db eventlog.DB, kind, id string) (json.RawMessage, error) {
	var raw json.RawMessage
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		var err error
		raw, err = Events(ctx, tx, AssessStream(kind, id))
		return err
	})
	return raw, err
}
