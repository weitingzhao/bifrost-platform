package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// The rules the runtime acts on (design v5 §5.2; proposal §2), each read at
// the database's clock in the transaction that acts on it. Without one in
// force, or with one whose text does not read, the strictest default holds:
// no concurrency rule dispatches nothing, no standing envelope starts no
// mission without the Owner.

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

// envelopeFit says whether a mission falls inside the standing envelope in
// force, and why not when it does not: an action above the envelope's, or a
// budget it does not state or the envelope does not cover. Which executors
// and hosts may work it is checked when its tasks are claimed.
func envelopeFit(env *StoredRule, p MissionProposal) (bool, string) {
	switch e := env; {
	case e == nil:
		return false, EnvelopePending
	case effectRank[p.MaxAction] > effectRank[e.Envelope.MaxAction]:
		return false, fmt.Sprintf("max_action %s is above the envelope's %s", p.MaxAction, e.Envelope.MaxAction)
	case p.MaxAttempts == 0 || p.MaxWallHours == 0:
		return false, "the mission states no budget and no budget:" + p.Tenant + " rule gives one"
	case p.MaxAttempts > e.Envelope.Budget.MaxAttempts || p.MaxWallHours > e.Envelope.Budget.MaxWallHours:
		return false, fmt.Sprintf("its budget (%d attempts, %d hours) is above the envelope's (%d, %d)", p.MaxAttempts, p.MaxWallHours,
			e.Envelope.Budget.MaxAttempts, e.Envelope.Budget.MaxWallHours)
	}
	return true, ""
}
