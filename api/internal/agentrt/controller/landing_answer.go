package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// LandingAnswer reads the landing question bound to this root and operation.
// S3 supplies the question binding; a generic task question is not sufficient.
// The callback returns semantic choices: "try_again", "do_not_merge", or
// "confirm_main_lost" (a push reported ok whose R main does not show, CUT
// §10.6). No
// answer (including rule answers) authorizes a release or changes eligibility.
//
// The answer is handled once and recorded (store.HandleLandingAnswer); the
// card is acknowledged only after that commits (CUT §8, review 647). An Ack
// that fails is retried on the next pass, which finds the record and does not
// act again. A refused answer comes back as a result, not an error. S3 builds
// the Owner's notice from the stored record (by question id), not from this
// return, so a crash after the Ack loses nothing (review 655 R4). Without a
// card answered on the console, with who and when, nothing happens and the
// result is empty.
func (c *Controller) LandingAnswer(ctx context.Context, root, after string, read func(context.Context, string, string) (store.Question, bool)) (store.LandingAnswerResult, error) {
	if read == nil {
		return store.LandingAnswerResult{}, nil
	}
	q, ok := read(ctx, root, after)
	if !ok || q.Status != store.QuestionAnswered || q.Channel != "console" || q.ID == "" || q.AnsweredBy == "" {
		return store.LandingAnswerResult{}, nil
	}
	at, err := time.Parse(time.RFC3339Nano, q.AnsweredAt)
	if err != nil {
		return store.LandingAnswerResult{}, fmt.Errorf("landing answer needs timestamp")
	}
	res, err := store.HandleLandingAnswer(ctx, c.DB, q, root, after, at)
	if err != nil {
		return res, err
	}
	// Without an Ack source the card stays unacknowledged (Acked false) and
	// the next pass returns the stored result again; S3 wires the source.
	ack := c.ack()
	if ack == nil {
		return res, nil
	}
	if err := ack(ctx, q.ID); err != nil {
		return res, fmt.Errorf("landing answer %s recorded (%s), ack failed: %w", q.ID, res.Outcome, err)
	}
	res.Acked = true
	return res, nil
}
