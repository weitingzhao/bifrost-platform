package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// The tuning rule (rules/tuning.go): the store's timeouts, retry counts and
// limits that the registry lists are read from the rule in force, once a
// pass or at the lease; without one (none in force, or one that does not
// read) every key keeps the code's default, the constant beside it. A nil
// *rules.Tuning is the defaults.

// tuningIn is the tuning rule in force in tx, nil when none is.
func tuningIn(ctx context.Context, tx pgx.Tx) (*rules.Tuning, error) {
	r, err := inForce(ctx, tx, rules.KindTuning)
	if err != nil || r == nil {
		return nil, err
	}
	return r.Tuning, nil
}

// ReadTuning is the tuning rule in force now, nil when none is: the
// controller reads it once a pass.
func ReadTuning(ctx context.Context, db DB) (*rules.Tuning, error) {
	var t *rules.Tuning
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		var err error
		t, err = tuningIn(ctx, tx)
		return err
	})
	return t, err
}

// leaseTTL is how long a lease lasts from now: the tuning rule's, or LeaseTTL.
func leaseTTL(ctx context.Context, tx pgx.Tx) (time.Duration, error) {
	t, err := tuningIn(ctx, tx)
	return t.Duration(rules.TuneLeaseTTL), err
}
