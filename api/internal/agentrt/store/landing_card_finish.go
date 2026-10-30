package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// Rule tightening takes the exclusive counterpart of this lock. Keep the
// shared lock from the final policy read through the question write and
// card receipt; checking only after the external write is too late.
func settleCardWrite(ctx context.Context, db DB, c CardRecord, write func(pgx.Tx) (string, error)) (string, error) {
	var result string
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		if c.Family == "stg" {
			enabled, err := lockStgCardPolicy(ctx, tx, c.Repo)
			if err != nil || !enabled {
				return err
			}
		}
		var err error
		result, err = write(tx)
		return err
	})
	return result, err
}

func settleCardAck(ctx context.Context, db DB, q CardQuestions, c CardRecord) (string, error) {
	if q.Ack == nil {
		return "", nil
	}
	return settleCardWrite(ctx, db, c, func(tx pgx.Tx) (string, error) {
		found, err := q.Ack(ctx, c.QuestionID)
		if err != nil {
			return "", err
		}
		if !found {
			return "gone_after_handled", finishLandingCardTx(ctx, tx, c.Repo, c, "landing_card_closed", "gone_after_handled")
		}
		return "acked", finishLandingCardTx(ctx, tx, c.Repo, c, "landing_card_acked", "")
	})
}

func settleCardWithdraw(ctx context.Context, db DB, q CardQuestions, c CardRecord, now time.Time) (string, error) {
	if q.Withdraw == nil {
		return "", nil
	}
	return settleCardWrite(ctx, db, c, func(tx pgx.Tx) (string, error) {
		applies, err := cardApplies(ctx, tx, c, now)
		if err != nil || applies {
			return "", err
		}
		status, err := q.Withdraw(ctx, c.QuestionID, "the landing or STG release trigger has moved on")
		if err != nil {
			return "", err
		}
		why := map[string]string{"withdrawn": "resolved", "expired": "expired", "gone": "lost"}[status]
		if why == "" {
			return "", nil // answered meanwhile: the next pass handles it
		}
		return why, finishLandingCardTx(ctx, tx, c.Repo, c, "landing_card_closed", why)
	})
}
