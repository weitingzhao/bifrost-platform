package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// RecordStgLeaseLost consumes a holder's asynchronous diagnostic. It never
// changes execution state, settles a batch, or refunds budget. Delayed notices
// for a previous try/closed batch are harmless; duplicate notices are no-ops.
func RecordStgLeaseLost(ctx context.Context, db DB, batch string, try, generation int64) error {
	if try < 1 || generation < 1 {
		return fmt.Errorf("invalid STG lease loss identity")
	}
	return InTx(ctx, db, func(tx pgx.Tx) error {
		if err := lockStream(ctx, tx, StgReleaseStream); err != nil {
			return err
		}
		s, err := StgReleaseState(ctx, tx)
		if err != nil {
			return err
		}
		b := s.Batches[batch]
		if b == nil || b.Phase == "CLOSED" || b.Try != try && !(b.Try == 0 && try == 1) {
			return nil
		}
		// Selection holds try 1's window before snapshot creates try 1.
		key := fmt.Sprintf("lease_lost/%d/%d", b.Try, generation)
		if _, exists := b.Diagnostics[key]; exists {
			return nil
		}
		_, err = appendStg(ctx, tx, stgBatchRecord("diag", batch, b.Try, "what", "lease_lost", "gen", generation))
		return err
	})
}
