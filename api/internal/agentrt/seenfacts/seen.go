package seenfacts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// External facts and step evaluation (design v5 §5.1.2 「done_rule」, §5.2
// 控制器「完成规则」「外部事实」). Approvals, questions, CI runs and deployments
// live outside the runtime; the controller reads them and records each change
// as an event, which is the immutable copy rules are evaluated against
// (answered questions are trimmed from their store later).

// The words the sources report for the states rules wait on.
const (
	ApprovalExecuted = "executed"
	QuestionAnswered = "answered"
	CISucceeded      = "Succeeded"
)

// ExternalStream is the event stream of external facts.
const ExternalStream = "external"

// read reads an outside record and returns the body to record for it; ok is
// false when it cannot be read now.
type read func(ctx context.Context) (body map[string]any, ok bool, err error)

// seen reads an outside record and records its state when it differs from
// the last one recorded under the same key, and reports whether it recorded.
// The read happens under the key's lock, after the last record is known, so
// two controllers record changes in the order they were read and a read
// that a newer one overtook is never recorded as a change back (review 151
// A5). A record that cannot be read records nothing.
func seen(ctx context.Context, db eventlog.DB, typ string, key map[string]any, r read) (bool, error) {
	keyDoc, err := canon.Encode(key)
	if err != nil {
		return false, err
	}
	var recorded bool
	err = eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		var e error
		recorded, e = recordIfChanged(ctx, tx, typ, string(keyDoc), r)
		return e
	})
	return recorded, err
}

func recordIfChanged(ctx context.Context, tx pgx.Tx, typ, keyDoc string, r read) (bool, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(742104, hashtext($1))`, typ+"|"+keyDoc); err != nil {
		return false, err
	}
	body, ok, err := r(ctx)
	if err != nil || !ok {
		return false, err
	}
	want, err := canon.Encode(body)
	if err != nil {
		return false, err
	}
	var last string
	err = tx.QueryRow(ctx, `SELECT body_json::text FROM agentrt.event WHERE event_type = $1 AND body_json @> $2::jsonb
	  ORDER BY event_id DESC LIMIT 1`, typ, keyDoc).Scan(&last)
	actor.Hook("seen", typ)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return false, err
	default:
		v, err := canon.Parse([]byte(last))
		if err != nil {
			return false, err
		}
		had, err := canon.Encode(v)
		if err != nil {
			return false, err
		}
		if string(had) == string(want) {
			return false, nil
		}
	}
	if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: ExternalStream, Type: typ, Actor: "controller", Body: body}); err != nil {
		return false, err
	}
	return true, nil
}

// SeeApproval reads an approval's status and records a change
// (approval_seen).
type ApprovalFact struct {
	Number                                    int64
	Status, Action, Tier, Env, Repo, Task, Op string
	CreatedAt, DecidedAt                      time.Time
	Expired                                   bool
}

func approvalSeenBody(a ApprovalFact) map[string]any {
	body := map[string]any{"approval": a.Number, "status": a.Status, "action": a.Action, "tier": a.Tier, "created_at": a.CreatedAt.UTC().Format(time.RFC3339), "expired": a.Expired}
	for k, v := range map[string]string{"env": a.Env, "repo": a.Repo, "task": a.Task, "op": a.Op} {
		if v != "" {
			body[k] = v
		}
	}
	if !a.DecidedAt.IsZero() {
		body["decided_at"] = a.DecidedAt.UTC().Format(time.RFC3339)
	}
	return body
}

func SeeApproval(ctx context.Context, db eventlog.DB, number int64, fact func(context.Context) (ApprovalFact, bool)) (bool, error) {
	return seen(ctx, db, "approval_seen", map[string]any{"approval": number}, func(ctx context.Context) (map[string]any, bool, error) {
		a, ok := fact(ctx)
		a.Number = number
		return approvalSeenBody(a), ok, nil
	})
}

// Question is a question as read from its store. An answered one carries the
// option, who answered and when, and the text when there is one.
type Question struct {
	Number                                                            int
	OwnerDuty, TaskID, CreatedAt, WithdrawnAt, WithdrawnBy, ExpiredAt string
	ID, Status, Option, Text, AnsweredBy, AnsweredAt                  string
	Options                                                           int    // how many options the card offers; 0 when the reader does not say
	Channel                                                           string // how it was answered: the console, or a rule (questions.RuleChannel)
	// OwnerAnswer is set only by the trusted question-store adapter after
	// the Console's admin authorization, never inferred from AnsweredBy.
	OwnerAnswer bool
	// ExpiresAt is when an open question expires; zero when the reader does
	// not say. The store marks it expired only at its next sweep, so an open
	// one past it can no longer be answered though it still reads open.
	ExpiresAt time.Time
	// Recommended is the option key the card recommends, as the card read
	// gives it; empty when it recommends none or the reader does not say.
	// question_seen keeps it (R5:5.7, the check value table); nothing
	// decides by it here.
	Recommended string
}

// SeeQuestion reads a question and records a change of its status and, once
// answered, the answer (question_seen): the copy the plan keeps after the
// question store trims the question. An answer without its option, who
// answered or when is refused, so the copy is never partial (review 151 B2).
func SeeQuestion(ctx context.Context, db eventlog.DB, id string, question func(context.Context) (Question, bool)) (bool, error) {
	return seen(ctx, db, "question_seen", map[string]any{"question": id}, func(ctx context.Context) (map[string]any, bool, error) {
		q, ok := question(ctx)
		if !ok {
			return nil, false, nil
		}
		return questionSeenBody(id, q)
	})
}

// questionSeenBody is the question_seen body of a question as read: its
// status and, once answered, the whole answer, and the option the card
// recommends when the reader gave it.
func questionSeenBody(id string, q Question) (map[string]any, bool, error) {
	if q.Status == QuestionAnswered && (q.Option == "" || q.AnsweredBy == "" || q.AnsweredAt == "") {
		return nil, false, fmt.Errorf("%w: question %s is answered but its option, answerer or time is missing", eventlog.ErrState, id)
	}
	body := map[string]any{"question": id, "status": q.Status}
	if q.Number > 0 {
		body["number"] = q.Number
	}
	for k, v := range map[string]string{"option": q.Option, "text": q.Text, "answered_by": q.AnsweredBy, "answered_at": q.AnsweredAt,
		"recommended": q.Recommended, "created_at": q.CreatedAt, "owner_duty": q.OwnerDuty, "task": q.TaskID, "channel": q.Channel, "withdrawn_at": q.WithdrawnAt, "withdrawn_by": q.WithdrawnBy, "expired_at": q.ExpiredAt} {
		if v != "" {
			body[k] = v
		}
	}
	return body, true, nil
}

// SeeCI reads the status of the CI run of a commit and records a change
// (ci_seen).
func SeeCI(ctx context.Context, db eventlog.DB, repo, commit string, status func(context.Context) (string, string, bool)) (bool, error) {
	return seen(ctx, db, "ci_seen", map[string]any{"repo": repo, "commit": commit}, func(ctx context.Context) (map[string]any, bool, error) {
		s, run, ok := status(ctx)
		body := map[string]any{"repo": repo, "commit": commit, "status": s}
		if run != "" {
			body["run"] = run
		}
		return body, ok, nil
	})
}

// SeeDeploy reads what an environment runs now and records a change
// (deploy_seen): the commit and the deploy reference (delivery run and image
// digests).
func SeeDeploy(ctx context.Context, db eventlog.DB, env string, deployed Deployed) (bool, error) {
	return seen(ctx, db, "deploy_seen", map[string]any{"env": env}, func(ctx context.Context) (map[string]any, bool, error) {
		commit, ref, ok := deployed(ctx, env)
		return map[string]any{"env": env, "commit": commit, "deploy_ref": ref}, ok, nil
	})
}

// Deployed reads what an environment runs now: its commit and its deploy
// reference. ok is false when the delivery record cannot be read.
type Deployed func(ctx context.Context, env string) (commit string, ref map[string]any, ok bool)
