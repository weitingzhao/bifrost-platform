package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// inForce is CurrentRule at the database's clock; a stored rule that does
// not read counts as none.
func inForce(ctx context.Context, tx pgx.Tx, id string) (*StoredRule, error) {
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return nil, err
	}
	r, err := CurrentRule(ctx, tx, id, now)
	if errors.Is(err, errBadRule) {
		return nil, nil
	}
	return r, err
}
