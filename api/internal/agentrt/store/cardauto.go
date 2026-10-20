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

// Answering cards by rule (TD-390, the Owner 2026-10-20 「STG 用 A」; TD-393,
// the Owner 2026-10-20 「STG 的规则等待按秒、设 5 秒，循环每 5 秒；STG 上所有种类
// 的运行时卡都归规则管；给不出推荐或规则和 AI 评估不一致时，STG 上选不改变任何东西的
// 那项…PROD 不变」). While a signed card-auto-answer rule is in force (STG only:
// PROD's rules have no such file, its platform does not wire the loop, and
// without the rule nothing is answered), a pass answers a card the runtime
// asked:
//   - of a kind the rule lists (cardkinds.go: a blocked task's card, a
//     planner's question, a plan change's, a plan import's, a recorder
//     import's, a mission's);
//   - still open on what it is about, and, but for a proposal's, of a mission
//     that does not need the Owner (scope_json.needs_owner: a mission that
//     must wait for him; cardFor);
//   - that has been open after_seconds unanswered;
//   - with the option it recommends when its AI assessment (assess.go,
//     TD-391) agrees or has not come, else with the option that changes
//     nothing: when it recommends nothing, or the assessment recommends
//     another option (ruleChoice), saying why on the card's answer.
//
// The answer is the rule's, never the Owner's: the question store records it
// as answered by "rule <id> v<version>" on the rule channel, a note on the
// card's mission (or its proposal's stream) says so, and what it moves names
// the rule (answerOf: a blocked card's owner_wait_closed by, the task and
// mission moves, a budget amended by the controller; a proposal's
// plan_change_decided by; a mission's move). Otherwise it acts as his would:
// the decision passes act on it and acknowledge it; a planner's question,
// whose answer nothing reads, is acknowledged here, so it does not hold a
// place in the question store, and a later pass finishes what an earlier left
// (finishRuleAnswer). A card is answered once: only open cards are answered,
// each in a transaction that reads the rule and holds what the card is about
// (answerOne).

// RuntimeCard is a card the runtime asked, as the question store holds it:
// open, or answered by a rule (By) and not acknowledged yet (Answered).
type RuntimeCard struct {
	ID, TaskID, Recommended string
	CreatedAt               time.Time
	Answered                bool
	By, Option              string // a rule's answer: the rule and the option it took
}

// CardRule is what answering cards by rule reads and writes beyond the
// database: the runtime's cards, an answer given as a rule while the rule
// holds (until) with why when the option is not the one the card
// recommends, and the acknowledgement of an answer.
type CardRule struct {
	Cards  func(ctx context.Context) ([]RuntimeCard, error)
	Answer func(ctx context.Context, id, option, rule string, until time.Time, why string) error
	Ack    func(ctx context.Context, id string) error
}

// The kinds of card a card-auto-answer rule may list; the others are in
// cardkinds.go.
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
		if now.Sub(card.CreatedAt) < time.Duration(rule.CardAuto.AfterSeconds)*time.Second {
			return nil
		}
		var ok bool
		if on, ok, err = cardFor(ctx, tx, card); err != nil || !ok || !slices.Contains(rule.CardAuto.Kinds, on.kind) {
			return err
		}
		// The card's AI assessment (TD-391), held while the rule reads it.
		st, err := cardAssessment(ctx, tx, card.ID)
		if err != nil {
			return err
		}
		option, why, said := ruleChoice(on.kind, card.Recommended, st, now)
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
		if err = c.Answer(ctx, card.ID, option, by, until, why); err != nil {
			return err
		}
		did = true
		return ruleNote(ctx, tx, on, by, card, option, now, said)
	})
	return on, did && err == nil, err
}

// ruleNote records where the card is (its mission, or its proposal's stream)
// that a rule answered it, with which option, and what its AI assessment
// said or why it took the option that changes nothing (said, "" when not
// known), unless that is recorded already.
func ruleNote(ctx context.Context, tx pgx.Tx, on cardOn, by string, card RuntimeCard, option string, now time.Time, said string) error {
	what := "the option it recommends"
	switch {
	case option == card.Recommended:
	case option == inaction(on.kind, card.Recommended):
		what = "the option that changes nothing"
	default:
		what = "the option its AI assessment recommends"
	}
	text := fmt.Sprintf("%s answered question %s with %s, %s, after %s unanswered", by, card.ID, option, what, waited(now.Sub(card.CreatedAt)))
	if said != "" {
		text += "; " + said
	}
	return noteRuleAnswer(ctx, tx, on, card.ID, text)
}

// noteRuleAnswer is the one way a rule's answer is noted: where the card is
// (the mission it was asked in, or its proposal's stream), that stream held
// from the look for a note to its writing, and only when there is none
// (reviews 443 B2, 444 B1, B2: the rule's own transaction, the decision that
// uses the answer and a later pass all come through here, so the answer is
// noted once, wherever its task is now).
func noteRuleAnswer(ctx context.Context, tx pgx.Tx, on cardOn, question, text string) error {
	if err := lockStream(ctx, tx, on.stream); err != nil {
		return err
	}
	if noted, err := ruleNoted(ctx, tx, on.stream, question); err != nil || noted {
		return err
	}
	hook("rule note: none yet", question)
	_, err := Append(ctx, tx, Event{Stream: on.stream, Type: "note", Actor: "controller", MissionID: on.mission,
		PlanNode: on.step, TaskID: on.task, Body: map[string]any{"text": text}})
	return err
}

// ruleNoted says whether the stream has a note that a rule answered the
// question. The caller holds the stream, so two passes never both find none
// (review 443 B2).
func ruleNoted(ctx context.Context, tx pgx.Tx, stream, question string) (bool, error) {
	var noted bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE event_type = 'note' AND event_stream_id = $1
	  AND body_json->>'text' LIKE 'rule % answered question ' || $2 || ' %')`, stream, question).Scan(&noted)
	return noted, err
}

// ensureRuleNote is the note of a blocked card a rule answered, written by
// whoever uses the answer when the rule's own transaction did not keep it:
// the card's decision, which acknowledges the answer, so the note is never
// left to a scan of unacknowledged answers (review 443 B1). mission is the
// one the card was asked in. Nothing for the Owner's.
func ensureRuleNote(ctx context.Context, tx pgx.Tx, mission string, t taskRow, by answerer, question, option string) error {
	return ensureRuleNoteOn(ctx, tx, cardOn{kind: CardBlocked, task: t.id, step: t.step, mission: mission, stream: MissionStream(mission)},
		by, question, option)
}

// ensureRuleNoteOn is ensureRuleNote for a card of any kind, where it is.
func ensureRuleNoteOn(ctx context.Context, tx pgx.Tx, on cardOn, by answerer, question, option string) error {
	if by.rule == "" {
		return nil
	}
	return noteRuleAnswer(ctx, tx, on, question, fmt.Sprintf("%s answered question %s with %s", by.rule, question, option))
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
		return ruleNote(ctx, tx, on, card.By, card, card.Option, now, "")
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

// answerer is who answered a card, as the moves its answer makes name them:
// the Owner, or the rule that answered with the card's own recommendation or
// the option that changes nothing (review 442 A3: then never the Owner), and
// the actor a budget it grows is amended as.
type answerer struct{ who, actor, rule string }

func answerOf(q Question) answerer {
	if q.Channel == questions.RuleChannel && q.AnsweredBy != "" {
		return answerer{who: "the " + q.AnsweredBy, actor: "controller", rule: q.AnsweredBy}
	}
	return answerer{who: "the Owner", actor: "owner"}
}
