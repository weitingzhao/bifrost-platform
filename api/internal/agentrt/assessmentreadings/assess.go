package assessmentreadings

import (
	"context"
	"encoding/json"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"

	"github.com/jackc/pgx/v5"
)

// AI assessments are advice only; historical stream readers retain these names.
const (
	AssessCard     = "card"
	AssessApproval = "approval"
)

// AssessStream names the item's recorded assessment stream.
func AssessStream(kind, id string) string { return "assess:" + kind + ":" + id }

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
