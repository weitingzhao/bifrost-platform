package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// An answered card the runtime has not acknowledged counts against its
// question quota (questions/create.go tally): eight such answers, long used,
// left PROD unable to ask the Owner anything (2026-11-06, 429 "read and ack
// Q1, ... first"). AckUsedAnswers acknowledges the answers no task waits on
// any more; an answer stays readable after it is acknowledged.
const (
	// AckUsedAnswersEvery is how often the facts loop runs the pass.
	AckUsedAnswersEvery = time.Minute
	// AnswerWaitLimit is how long after an answer its task may still be
	// waiting to use it.
	AnswerWaitLimit = 24 * time.Hour
)

// taskWait reads an answered card's task (store.TaskWait); tests replace it.
var taskWait = store.TaskWait

// AckUsedAnswers is one pass over the runtime's answered cards it has not
// acknowledged (Sources.Unacked): each is acknowledged (Sources.Ack) when
// its task is accepted, blocked, cancelled or held by a stop, when it names
// no task or one that does not exist, or when its answer is older than
// AnswerWaitLimit. A card whose task may still wait on it is left alone, and
// so is one a rule answered within AnswerWaitLimit (AnswerByRule finishes and
// acknowledges it). It returns how many it acknowledged.
func (c *Controller) AckUsedAnswers(ctx context.Context) (int, error) {
	if c.Sources.Unacked == nil || c.Sources.Ack == nil {
		return 0, nil
	}
	type list struct {
		cards []store.AnsweredCard
		err   error
	}
	x := bounded(c, ctx, func(ctx context.Context) list { cs, err := c.Sources.Unacked(ctx); return list{cs, err} })
	if x.err != nil {
		return 0, fmt.Errorf("listing the runtime's unacknowledged answers: %w", x.err)
	}
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	ack := c.ack()
	acked := 0
	var errs []error
	for _, q := range x.cards {
		why, err := c.answerUsed(ctx, q, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("Q%d: %w", q.Number, err))
			continue
		}
		if why == "" {
			continue
		}
		if err := ack(ctx, q.ID); err != nil {
			if !errors.Is(err, store.ErrQuestionGone) {
				errs = append(errs, fmt.Errorf("acknowledging Q%d: %w", q.Number, err))
			}
			continue
		}
		acked++
		c.infof("agentrt acknowledged Q%d's answer: %s", q.Number, why)
	}
	return acked, errors.Join(errs...)
}

// answerUsed says why the runtime no longer waits on the answer of q, or ""
// when its task may still.
func (c *Controller) answerUsed(ctx context.Context, q store.AnsweredCard, now time.Time) (string, error) {
	if !q.AnsweredAt.IsZero() && now.Sub(q.AnsweredAt) > AnswerWaitLimit {
		return fmt.Sprintf("answered %s ago, more than %s", now.Sub(q.AnsweredAt).Round(time.Minute), AnswerWaitLimit), nil
	}
	if q.ByRule {
		return "", nil // the rule's pass acknowledges it once it has finished it
	}
	if q.TaskID == "" {
		return "it names no task", nil
	}
	state, stopped, found, err := taskWait(ctx, c.DB, q.TaskID)
	switch {
	case err != nil:
		return "", err
	case !found:
		return fmt.Sprintf("its task %s does not exist", q.TaskID), nil
	case state == "accepted" || state == "blocked" || state == "cancelled":
		return fmt.Sprintf("its task %s is %s", q.TaskID, state), nil
	case stopped:
		return fmt.Sprintf("a stop holds its task %s", q.TaskID), nil
	}
	return "", nil
}

// ackUsedAnswersDue runs AckUsedAnswers at most once every
// AckUsedAnswersEvery; the facts loop calls it on each pass.
func (c *Controller) ackUsedAnswersDue(ctx context.Context) (int, error) {
	now := time.Now()
	if !c.answersAckedAt.IsZero() && now.Sub(c.answersAckedAt) < AckUsedAnswersEvery {
		return 0, nil
	}
	c.answersAckedAt = now
	return c.AckUsedAnswers(ctx)
}

func (c *Controller) infof(format string, args ...any) {
	if c.Infof != nil {
		c.Infof(format, args...)
		return
	}
	c.logf(format, args...)
}
