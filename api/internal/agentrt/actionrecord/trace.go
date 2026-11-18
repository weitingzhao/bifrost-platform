package actionrecord

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// An action trace copies the materials the executor already used for one
// action (the request, the decision, the result: the server's actionTrace,
// redacted), on the stream action-trace, for the review of automatic
// decisions afterwards (the Owner keeps their trail seven days). It never
// authorizes anything, and a failed append does not change the action.
//
// Its event type is this package's, action_traced {text} (migration 0032,
// 2026-11-17: one owning domain per event type; it was a note on the same
// stream, whose rows stay).
const tracedType = "action_traced"

func init() {
	eventlog.Declare(eventlog.Vocabulary{
		Types:  []string{tracedType},
		Bodies: map[string][]eventlog.Field{tracedType: {{Name: "text", Kind: eventlog.Str}}},
	})
}

// AppendTrace records one action trace, raw being its JSON.
func AppendTrace(ctx context.Context, db eventlog.DB, raw string) error {
	return eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		_, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "action-trace", Type: tracedType, Actor: "controller", Body: map[string]any{"text": raw}})
		return err
	})
}
