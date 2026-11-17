package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
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
//     TD-391) agrees or has not come, and the option that changes nothing
//     when an assessment that has come recommends another; a card that
//     recommends nothing takes the option of an assessment that has come,
//     else the option that changes nothing (ruleChoice); an option it does
//     not recommend says why on the card's answer. Under a rule that says
//     assessment: veto (rules.CardAuto.AssessmentVeto), a card that
//     recommends nothing takes the option that changes nothing whatever its
//     assessment says, and a card without the option the rule would take is
//     left open for the Owner.
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
	// Options is the keys of the card's options in order, as the question
	// store gave them (a, b, …): a rule under assessment: veto answers only
	// with one of them.
	Options    []string
	Answered   bool
	By, Option string // a rule's answer: the rule and the option it took
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

// ensureRuleNoteOn is ensureRuleNote for a card of any kind, where it is.
func ensureRuleNoteOn(ctx context.Context, tx pgx.Tx, on cardOn, by answerer, question, option string) error {
	if by.rule == "" {
		return nil
	}
	return noteRuleAnswer(ctx, tx, on, question, fmt.Sprintf("%s answered question %s with %s", by.rule, question, option))
}

// answerer is who answered a card, as the moves its answer makes name them:
// the Owner, or the rule that answered with the card's own recommendation or
// the option that changes nothing (review 442 A3: then never the Owner), and
// the actor a budget it grows is amended as.
type answerer struct{ who, actor, rule string }

func answerOf(q seenfacts.Question) answerer {
	if q.Channel == RuleAnswerChannel && q.AnsweredBy != "" {
		return answerer{who: "the " + q.AnsweredBy, actor: "controller", rule: q.AnsweredBy}
	}
	return answerer{who: "the Owner", actor: "owner"}
}
