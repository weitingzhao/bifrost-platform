package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// LandingAnswer reads the landing question bound to this root and operation.
// S3 supplies the question binding; a generic task question is not sufficient.
// The callback returns semantic choices, "try_again" or "do_not_merge". No
// answer (including rule answers) authorizes a release or changes eligibility.
func (c *Controller) LandingAnswer(ctx context.Context, root, after string, read func(context.Context, string, string) (store.Question, bool)) error {
	if read == nil {
		return nil
	}
	q, ok := read(ctx, root, after)
	if !ok || q.Status != store.QuestionAnswered || q.Channel != "console" || q.ID == "" || q.AnsweredBy == "" {
		return nil
	}
	at, err := time.Parse(time.RFC3339Nano, q.AnsweredAt)
	if err != nil {
		return fmt.Errorf("landing answer needs timestamp")
	}
	return store.InTx(ctx, c.DB, func(tx pgx.Tx) error {
		return store.ApplyLandingAnswer(ctx, tx, root, after, q.Option, q.Text, q.AnsweredBy, at)
	})
}
