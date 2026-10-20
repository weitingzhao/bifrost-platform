package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// Answering cards by rule (TD-390, the Owner 2026-10-20 「STG 用 A：TD-389
// 上线后，STG 上带推荐的运行时卡 30 分钟没人答就按推荐执行；走查要测我按钮的卡标
// 「需要本人」；PROD 不变」). While a signed card-auto-answer rule is in force
// (STG only: PROD's rules have no such file, and without the rule nothing is
// answered), a pass answers a card the runtime asked:
//   - of a kind the rule lists: a blocked task's card (blocked, its
//     owner_wait_opened still open) or a planner's question (plan_question:
//     a card naming a planning task); never a plan change's or a mission's;
//   - that recommends an option, with that option and no other;
//   - whose mission is active or paused and does not need the Owner
//     (scope_json.needs_owner, set when a walk tests his buttons);
//   - that has been open after_minutes unanswered.
//
// The answer is the rule's, never the Owner's: the question store records it
// as answered by "rule <id> v<version>" on the rule channel, and a note on the
// mission says so. From there on it is an answer like his: DecideBlocked acts
// on a blocked card's and acknowledges it; a planner's question, whose answer
// nothing reads (the work goes on with the recommended option meanwhile), is
// acknowledged here, so it does not hold a place in the question store.
// A card is answered once: only open cards are listed.

// RuntimeCard is an open card the runtime asked, as the question store holds
// it.
type RuntimeCard struct {
	ID, TaskID, Recommended string
	CreatedAt               time.Time
}

// CardRule is what answering cards by rule reads and writes beyond the
// database: the runtime's open cards, an answer given as a rule, and the
// acknowledgement of an answer.
type CardRule struct {
	Cards  func(ctx context.Context) ([]RuntimeCard, error)
	Answer func(ctx context.Context, id, option, rule string) error
	Ack    func(ctx context.Context, id string) error
}

// The kinds of card a card-auto-answer rule may list.
const (
	CardBlocked      = "blocked"
	CardPlanQuestion = "plan_question"
)

// AnswerByRule is one pass answering the runtime's cards by the rule in
// force; it returns how many it answered. A card it cannot answer now is
// tried again by the next pass.
func AnswerByRule(ctx context.Context, db DB, c CardRule) (int, error) {
	if c.Cards == nil || c.Answer == nil {
		return 0, nil
	}
	var rule *StoredRule
	var now time.Time
	if err := InTx(ctx, db, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return err
		}
		var err error
		rule, err = inForce(ctx, tx, rules.KindCardAuto)
		return err
	}); err != nil || rule == nil {
		return 0, err
	}
	cards, err := c.Cards(ctx)
	if err != nil {
		return 0, err
	}
	wait := time.Duration(rule.CardAuto.AfterMinutes) * time.Minute
	by := fmt.Sprintf("rule %s v%d", rule.ID, rule.Version)
	n := 0
	var errs []error
	for _, card := range cards {
		if card.Recommended == "" || now.Sub(card.CreatedAt) < wait {
			continue
		}
		on, ok, err := cardFor(ctx, db, card)
		if err != nil {
			errs = append(errs, fmt.Errorf("card %s: %w", card.ID, err))
			continue
		}
		if !ok || !slices.Contains(rule.CardAuto.Kinds, on.kind) {
			continue
		}
		if err = c.Answer(ctx, card.ID, card.Recommended, by); err != nil {
			errs = append(errs, fmt.Errorf("answer %s: %w", card.ID, err))
			continue
		}
		n++
		text := fmt.Sprintf("%s answered question %s with %s, the option it recommends, after %d minutes unanswered",
			by, card.ID, card.Recommended, int(now.Sub(card.CreatedAt)/time.Minute))
		if err = InTx(ctx, db, func(tx pgx.Tx) error {
			_, err := Append(ctx, tx, Event{Stream: MissionStream(on.mission), Type: "note", Actor: "controller", MissionID: on.mission,
				PlanNode: on.step, TaskID: on.task, Body: map[string]any{"text": text}})
			return err
		}); err != nil {
			errs = append(errs, fmt.Errorf("note of %s: %w", card.ID, err))
		}
		if on.kind == CardPlanQuestion && c.Ack != nil {
			if err = c.Ack(ctx, card.ID); err != nil && !errors.Is(err, ErrQuestionGone) {
				errs = append(errs, fmt.Errorf("acknowledge %s: %w", card.ID, err))
			}
		}
	}
	return n, errors.Join(errs...)
}

// cardOn is the card a question is in the runtime: its kind, its task, the
// task's step and the mission the card was asked in.
type cardOn struct{ kind, task, step, mission string }

// cardFor reads which card a question is; ok is false for one no rule may
// answer: not the runtime's blocked card or a planner's question, a blocked
// card the runtime has closed, or one of a mission that is not open or needs
// the Owner.
func cardFor(ctx context.Context, db DB, card RuntimeCard) (cardOn, bool, error) {
	var on cardOn
	ok := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT o.task_id, COALESCE(t.plan_node_id, ''), o.mission_id FROM agentrt.event o
		  JOIN agentrt.task t ON t.task_id = o.task_id
		  WHERE o.event_type = 'owner_wait_opened' AND o.body_json->>'question_id' = $1 AND `+cardOpen, card.ID).Scan(&on.task, &on.step, &on.mission)
		switch {
		case err == nil:
			on.kind = CardBlocked
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		case card.TaskID == "":
			return nil
		default:
			// A planner's question names its planning task; a blocked card the
			// runtime has closed names its task too, and is no planner's.
			var role string
			var blocked bool
			err = tx.QueryRow(ctx, `SELECT t.role, COALESCE(t.plan_node_id, ''), t.mission_id,
			    EXISTS (SELECT 1 FROM agentrt.event o WHERE o.event_type = 'owner_wait_opened' AND o.body_json->>'question_id' = $2)
			  FROM agentrt.task t WHERE t.task_id = $1`, card.TaskID, card.ID).Scan(&role, &on.step, &on.mission, &blocked)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && (role != "plan" || blocked)) {
				return nil
			}
			if err != nil {
				return err
			}
			on.kind, on.task = CardPlanQuestion, card.TaskID
		}
		var state, needsOwner string
		if err = tx.QueryRow(ctx, `SELECT state, COALESCE(scope_json->>'needs_owner', '') FROM agentrt.mission WHERE mission_id = $1`,
			on.mission).Scan(&state, &needsOwner); err != nil {
			return err
		}
		ok = (state == "active" || state == "paused") && needsOwner == ""
		return nil
	})
	return on, ok, err
}
