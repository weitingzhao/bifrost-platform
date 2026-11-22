// Package answerack clears the runtime's inbox of the Owner's answers it no
// longer waits on: a pass of the controller's facts loop (agentrt/controller
// runs it as a step; the assembly, server, binds it).
//
// Origin: 2026-11-19. The pass was the controller's own (ackanswers.go),
// which put the owner domain's question cards inside a package of other
// domains; it moved here unchanged so that each package belongs to one
// domain (OWNER_LAW #28).
package questionbridge

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/ownercard"
)

// An answered card the runtime had not acknowledged counted against its
// question quota: eight such answers, long used, left PROD unable to ask the
// Owner anything (2026-11-06, 429 "read and ack Q1, ... first"). That quota
// is gone (questions/create.go counts open questions only); AckUsedAnswers
// still acknowledges the answers no task waits on any more, so the inbox
// shows what is new; an answer stays readable after it is acknowledged.
const (
	// AckUsedAnswersEvery is how often the facts loop runs the pass.
	AckUsedAnswersEvery = time.Minute
	// AnswerWaitLimit is how long after an answer its task may still be
	// waiting to use it.
	AnswerWaitLimit = 24 * time.Hour
)

// taskWait reads an answered card's task (TaskWait); tests replace it.
var taskWait = TaskWait

// Acker holds what the pass needs.
type Acker struct {
	DB eventlog.DB
	// Unacked lists the runtime's answered cards it has not acknowledged
	// (AckUsedAnswers); nil acknowledges none of them.
	Unacked func(ctx context.Context) ([]AnsweredCard, error)
	// Ack tells the question store the runtime used the answer of a card it
	// asked; nil acknowledges nothing.
	Ack func(ctx context.Context, id string) error
	// Now is the clock the pass reads, time.Now when nil.
	Now func() time.Time
	// ReadTimeout bounds each source read; zero means 10 seconds.
	ReadTimeout time.Duration
	Logf        func(format string, args ...any)
	// Infof logs what the pass did that needs no one's attention (an answer
	// it acknowledged); nil uses Logf.
	Infof func(format string, args ...any)
	// ackedAt is the last AckUsedAnswers pass of the facts loop.
	ackedAt time.Time
}

// AckUsedAnswers is one pass over the runtime's answered cards it has not
// acknowledged (Unacked): each is acknowledged (Ack) when its task is
// accepted, blocked, cancelled or held by a stop, when it names no task or
// one that does not exist, or when its answer is older than AnswerWaitLimit.
// A card whose task may still wait on it is left alone, and so is one a rule
// answered within AnswerWaitLimit (AnswerByRule finishes and acknowledges
// it). It returns how many it acknowledged.
func (a *Acker) AckUsedAnswers(ctx context.Context) (int, error) {
	if a.Unacked == nil || a.Ack == nil {
		return 0, nil
	}
	type list struct {
		cards []AnsweredCard
		err   error
	}
	x := bounded(a, ctx, func(ctx context.Context) list { cs, err := a.Unacked(ctx); return list{cs, err} })
	if x.err != nil {
		return 0, fmt.Errorf("listing the runtime's unacknowledged answers: %w", x.err)
	}
	now := time.Now()
	if a.Now != nil {
		now = a.Now()
	}
	ack := a.ack()
	acked := 0
	var errs []error
	for _, q := range x.cards {
		why, err := a.answerUsed(ctx, q, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("Q%d: %w", q.Number, err))
			continue
		}
		if why == "" {
			continue
		}
		if err := ack(ctx, q.ID); err != nil {
			if !errors.Is(err, ownercard.ErrQuestionGone) {
				errs = append(errs, fmt.Errorf("acknowledging Q%d: %w", q.Number, err))
			}
			continue
		}
		acked++
		a.infof("agentrt acknowledged Q%d's answer: %s", q.Number, why)
	}
	return acked, errors.Join(errs...)
}

// answerUsed says why the runtime no longer waits on the answer of q, or ""
// when its task may still.
func (a *Acker) answerUsed(ctx context.Context, q AnsweredCard, now time.Time) (string, error) {
	if !q.AnsweredAt.IsZero() && now.Sub(q.AnsweredAt) > AnswerWaitLimit {
		return fmt.Sprintf("answered %s ago, more than %s", now.Sub(q.AnsweredAt).Round(time.Minute), AnswerWaitLimit), nil
	}
	if q.ByRule {
		return "", nil // the rule's pass acknowledges it once it has finished it
	}
	if q.TaskID == "" {
		return "it names no task", nil
	}
	state, stopped, found, err := taskWait(ctx, a.DB, q.TaskID)
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

// AckUsedAnswersDue runs AckUsedAnswers at most once every
// AckUsedAnswersEvery; the facts loop calls it on each pass.
func (a *Acker) AckUsedAnswersDue(ctx context.Context) (int, error) {
	now := time.Now()
	if !a.ackedAt.IsZero() && now.Sub(a.ackedAt) < AckUsedAnswersEvery {
		return 0, nil
	}
	a.ackedAt = now
	return a.AckUsedAnswers(ctx)
}

// ack acknowledges an answer with the per-read timeout.
func (a *Acker) ack() func(context.Context, string) error {
	return func(ctx context.Context, id string) error {
		return bounded(a, ctx, func(ctx context.Context) error { return a.Ack(ctx, id) })
	}
}

// bounded runs a source read with the per-read timeout.
func bounded[T any](a *Acker, ctx context.Context, read func(context.Context) T) T {
	d := a.ReadTimeout
	if d <= 0 {
		d = 10 * time.Second
	}
	rctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	return read(rctx)
}

func (a *Acker) infof(format string, args ...any) {
	if a.Infof != nil {
		a.Infof(format, args...)
		return
	}
	if a.Logf != nil {
		a.Logf(format, args...)
	}
}
