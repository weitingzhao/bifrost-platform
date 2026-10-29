package store

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// RefreshWaitingForTest exercises the shared locked generation transition.
// HTTP executor/pin authentication requires C-0's schema and is separate.
func RefreshWaitingForTest(ctx context.Context, db DB, root, op, head string) (bool, error) {
	wrote := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		mission, err := lockWaitingRoot(ctx, tx, root)
		if err != nil {
			return err
		}
		wrote, err = refreshWaitingLocked(ctx, tx, root, op, mission, head)
		return err
	})
	return wrote, err
}
