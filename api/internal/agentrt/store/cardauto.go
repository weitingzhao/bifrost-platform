package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
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
// as answered by "rule <id> v<version>" on the rule channel, a note on the
// mission says so, and what it moves names the rule (answerOf: the card's
// owner_wait_closed by, the task and mission moves, a budget amended by the
// controller). Otherwise it acts as his would: DecideBlocked acts on a
// blocked card's and acknowledges it; a planner's question, whose answer
// nothing reads (the work goes on with the recommended option meanwhile), is
// acknowledged here, so it does not hold a place in the question store, and
// a later pass finishes what an earlier left (finishRuleAnswer). A card is
// answered once: only open cards are answered, each in a transaction that
// reads the rule and holds the card's task and mission (answerOne). Only
// STG's platform answers at all (server/agentrt_wire.go).

// RuntimeCard is a card the runtime asked, as the question store holds it:
// open, or answered by a rule (By) and not acknowledged yet (Answered).
type RuntimeCard struct {
	ID, TaskID, Recommended string
	CreatedAt               time.Time
	Answered                bool
	By                      string
}

// CardRule is what answering cards by rule reads and writes beyond the
// database: the runtime's cards, an answer given as a rule while the rule
// holds (until), and the acknowledgement of an answer.
type CardRule struct {
	Cards  func(ctx context.Context) ([]RuntimeCard, error)
	Answer func(ctx context.Context, id, option, rule string, until time.Time) error
	Ack    func(ctx context.Context, id string) error
}

// The kinds of card a card-auto-answer rule may list.
const (
	CardBlocked      = "blocked"
	CardPlanQuestion = "plan_question"
)

// AnswerByRule is one pass answering the runtime's cards by the rule in
// force, and finishing what an earlier pass left of a card a rule answered
// (its note, a planner question's acknowledgement; review 442 B1). It returns
// how many it answered. A card it cannot answer now is tried again by the
// next pass.
func AnswerByRule(ctx context.Context, db DB, c CardRule) (int, error) {
	if c.Cards == nil || c.Answer == nil {
		return 0, nil
	}
	cards, err := c.Cards(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	var errs []error
	for _, card := range cards {
		if card.Answered {
			errs = append(errs, finishRuleAnswer(ctx, db, c, card))
			continue
		}
		on, did, err := answerOne(ctx, db, c, card)
		if err != nil {
			errs = append(errs, fmt.Errorf("card %s: %w", card.ID, err))
			continue
		}
		if !did {
			continue
		}
		n++
		if on.kind == CardPlanQuestion && c.Ack != nil {
			// Nothing reads a planner question's answer; a failure is
			// finished by the next pass (finishRuleAnswer).
			if err = c.Ack(ctx, card.ID); err != nil && !errors.Is(err, ErrQuestionGone) {
				errs = append(errs, fmt.Errorf("acknowledge %s: %w", card.ID, err))
			}
		}
	}
	return n, errors.Join(errs...)
}

// answerOne answers one open card if the rule in force lets it, in one
// transaction (review 442 A1, A2): the rule's versions shared (a new version,
// a tightening included, waits for this answer, and an answer after it reads
// it), the rule read and the card's age checked against it, then the card's
// task and mission locked in the kernel's order and the card checked again
// under them (cardFor); the answer is written, while the rule holds, then a
// note on the mission says so.
func answerOne(ctx context.Context, db DB, c CardRule, card RuntimeCard) (cardOn, bool, error) {
	var on cardOn
	did := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		did = false
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared($1, hashtext($2))`, ruleLock, rules.KindCardAuto); err != nil {
			return err
		}
		rule, err := inForce(ctx, tx, rules.KindCardAuto)
		if err != nil || rule == nil {
			return err
		}
		var now time.Time
		if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return err
		}
		if card.Recommended == "" || now.Sub(card.CreatedAt) < time.Duration(rule.CardAuto.AfterMinutes)*time.Minute {
			return nil
		}
		var ok bool
		if on, ok, err = cardFor(ctx, tx, card); err != nil || !ok || !slices.Contains(rule.CardAuto.Kinds, on.kind) {
			return err
		}
		// The rule holds until it expires or a later version stored already
		// takes effect (review 443 A2: the question store may be reached
		// after that instant).
		until := rule.Expires
		var next *time.Time
		if err = tx.QueryRow(ctx, `SELECT min(effective_at) FROM agentrt.rule WHERE rule_id = $1 AND rule_version > $2`, rule.ID, rule.Version).
			Scan(&next); err != nil {
			return err
		}
		if next != nil && next.Before(until) {
			until = *next
		}
		by := fmt.Sprintf("rule %s v%d", rule.ID, rule.Version)
		if err = c.Answer(ctx, card.ID, card.Recommended, by, until); err != nil {
			return err
		}
		did = true
		return ruleNote(ctx, tx, on, by, card, now)
	})
	return on, did && err == nil, err
}

// ruleNote records on the card's mission that a rule answered it, unless
// that is recorded already.
func ruleNote(ctx context.Context, tx pgx.Tx, on cardOn, by string, card RuntimeCard, now time.Time) error {
	return noteRuleAnswer(ctx, tx, on.mission, on.step, on.task, card.ID, fmt.Sprintf(
		"%s answered question %s with %s, the option it recommends, after %d minutes unanswered",
		by, card.ID, card.Recommended, int(now.Sub(card.CreatedAt)/time.Minute)))
}

// noteRuleAnswer is the one way a rule's answer is noted: on the mission the
// card was asked in, its stream held from the look for a note to its writing,
// and only when there is none (reviews 443 B2, 444 B1, B2: the rule's own
// transaction, the decision that uses the answer and a later pass all come
// through here, so the answer is noted once, wherever its task is now).
func noteRuleAnswer(ctx context.Context, tx pgx.Tx, mission, step, task, question, text string) error {
	if err := lockStream(ctx, tx, MissionStream(mission)); err != nil {
		return err
	}
	if noted, err := ruleNoted(ctx, tx, mission, question); err != nil || noted {
		return err
	}
	hook("rule note: none yet", question)
	_, err := Append(ctx, tx, Event{Stream: MissionStream(mission), Type: "note", Actor: "controller", MissionID: mission,
		PlanNode: step, TaskID: task, Body: map[string]any{"text": text}})
	return err
}

// ruleNoted says whether the mission has a note that a rule answered the
// question. The caller holds the mission's stream, so two passes never both
// find none (review 443 B2).
func ruleNoted(ctx context.Context, tx pgx.Tx, mission, question string) (bool, error) {
	var noted bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE event_type = 'note' AND mission_id = $1
	  AND body_json->>'text' LIKE 'rule % answered question ' || $2 || ' %')`, mission, question).Scan(&noted)
	return noted, err
}

// ensureRuleNote is the note of a card a rule answered, written by whoever
// uses the answer when the rule's own transaction did not keep it: the card's
// decision, which acknowledges the answer, so the note is never left to a
// scan of unacknowledged answers (review 443 B1). mission is the one the card
// was asked in. Nothing for the Owner's.
func ensureRuleNote(ctx context.Context, tx pgx.Tx, mission string, t taskRow, by answerer, question, option string) error {
	if by.rule == "" {
		return nil
	}
	return noteRuleAnswer(ctx, tx, mission, t.step, t.id, question, fmt.Sprintf("%s answered question %s with %s, the option it recommends",
		by.rule, question, option))
}

// finishRuleAnswer finishes a card a rule answered that the runtime has not
// acknowledged: the note, when the answer's transaction did not keep it, and
// a planner question's acknowledgement (a blocked card's answer is
// DecideBlocked's to use and acknowledge).
func finishRuleAnswer(ctx context.Context, db DB, c CardRule, card RuntimeCard) error {
	var on cardOn
	var ok bool
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		var err error
		if on, ok, err = cardKind(ctx, tx, card); err != nil || !ok {
			return err
		}
		var now time.Time
		if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return err
		}
		return ruleNote(ctx, tx, on, card.By, card, now)
	})
	if err != nil {
		return fmt.Errorf("card %s: %w", card.ID, err)
	}
	if ok && on.kind == CardPlanQuestion && c.Ack != nil {
		if err = c.Ack(ctx, card.ID); err != nil && !errors.Is(err, ErrQuestionGone) {
			return fmt.Errorf("acknowledge %s: %w", card.ID, err)
		}
	}
	return nil
}

// cardOn is the card a question is in the runtime: its kind, its task, the
// task's step and the mission the card was asked in.
type cardOn struct{ kind, task, step, mission string }

// cardKind reads which card a question is, without locks: a blocked task's
// card (its owner_wait_opened), or a planner's question (a card naming a
// planning task, with no owner_wait_opened); ok is false for anything else
// (a plan change's or a mission's card).
func cardKind(ctx context.Context, tx pgx.Tx, card RuntimeCard) (cardOn, bool, error) {
	var on cardOn
	err := tx.QueryRow(ctx, `SELECT o.task_id, COALESCE(t.plan_node_id, ''), o.mission_id FROM agentrt.event o
	  JOIN agentrt.task t ON t.task_id = o.task_id
	  WHERE o.event_type = 'owner_wait_opened' AND o.body_json->>'question_id' = $1`, card.ID).Scan(&on.task, &on.step, &on.mission)
	if err == nil {
		on.kind = CardBlocked
		return on, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) || card.TaskID == "" {
		return on, false, ignoreNoRows(err)
	}
	var role string
	err = tx.QueryRow(ctx, `SELECT role, COALESCE(plan_node_id, ''), mission_id FROM agentrt.task WHERE task_id = $1`, card.TaskID).
		Scan(&role, &on.step, &on.mission)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && role != "plan") {
		return on, false, nil
	}
	on.kind, on.task = CardPlanQuestion, card.TaskID
	return on, err == nil, err
}

func ignoreNoRows(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

// cardFor reads which card a question is and whether a rule may answer it,
// with the card's task locked, then its mission shared (the kernel's order):
// a blocked card must still be open in the runtime, its task blocked and
// still in the mission the card was asked in (review 442 A1: a task moved to
// a mission that needs the Owner keeps its old card from the rule); either
// kind's mission must be active or paused and not need the Owner. Under these
// locks no plan change moves the task and no close ends the mission before
// the answer is written.
func cardFor(ctx context.Context, tx pgx.Tx, card RuntimeCard) (cardOn, bool, error) {
	on, ok, err := cardKind(ctx, tx, card)
	if err != nil || !ok {
		return on, false, err
	}
	t, err := readTask(ctx, tx, on.task, true)
	if err != nil {
		return on, false, err
	}
	if on.kind == CardBlocked {
		var open bool
		if err = tx.QueryRow(ctx, `SELECT NOT EXISTS (SELECT 1 FROM agentrt.event c WHERE c.task_id = $1 AND c.event_type = 'owner_wait_closed'
		  AND c.body_json->>'question_id' = $2)`, on.task, card.ID).Scan(&open); err != nil {
			return on, false, err
		}
		if !open || t.state != "blocked" || t.mission != on.mission {
			return on, false, nil
		}
	}
	var state, needsOwner string
	if err = tx.QueryRow(ctx, `SELECT state, COALESCE(scope_json->>'needs_owner', '') FROM agentrt.mission WHERE mission_id = $1 FOR SHARE`,
		t.mission).Scan(&state, &needsOwner); err != nil {
		return on, false, err
	}
	return on, (state == "active" || state == "paused") && needsOwner == "", nil
}

// answerer is who answered a card, as the moves its answer makes name them:
// the Owner, or the rule that answered with the card's own recommendation
// (review 442 A3: then never the Owner), and the actor a budget it grows is
// amended as.
type answerer struct{ who, actor, rule string }

func answerOf(q Question) answerer {
	if q.Channel == questions.RuleChannel && q.AnsweredBy != "" {
		return answerer{who: "the " + q.AnsweredBy, actor: "controller", rule: q.AnsweredBy}
	}
	return answerer{who: "the Owner", actor: "owner"}
}
