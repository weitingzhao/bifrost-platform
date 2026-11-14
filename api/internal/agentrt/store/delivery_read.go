package store

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
)

// ReadDelivery: one read of a commit's delivery stages. The commit is one an
// external work named (a heartbeat or its submitted event); its stages come
// from the recorded pipeline runs of that exact commit
// (delivery_external.go). Reads by task, and of commits only a landing
// operation of the retired dispatch lane named, ended with that lane
// (LANE-BIZ-ALIGN, 2026-11-13).

// DeliveryQuery names a change by Commit, a 7 to 40 character lowercase hex
// prefix.
type DeliveryQuery struct {
	Commit string
	// Production is supplied by the server's trusted release reader, never URL input.
	Production *DeliveryProduction
}

var deliveryCommitPrefix = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// deliveryTaskID is a task id as the boundary precheck accepts it.
var deliveryTaskID = regexp.MustCompile(`^t_[0-9A-HJKMNP-TV-Z]{26}$`)

// ReadDelivery reads the delivery stages of q's commit. pgx.ErrNoRows: no
// external work names it.
func ReadDelivery(ctx context.Context, db DB, q DeliveryQuery) (map[string]any, error) {
	if !deliveryCommitPrefix.MatchString(q.Commit) {
		return nil, fmt.Errorf("%w: commit must be 7 to 40 lowercase hex characters", ErrInvalid)
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return nil, err
	}
	out, err := readExternalDelivery(ctx, tx, q, now)
	if err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}
