package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// The kinds of card the runtime asks, as the card-auto-answer rule tells
// them apart, and the option of each that changes nothing (TD-393, the Owner
// 2026-10-20 「STG 上所有种类的运行时卡都归规则管；给不出推荐或规则和 AI 评估不一致
// 时，STG 上选不改变任何东西的那项」):
//   - blocked, a blocked task's card: b, "Do not do this task";
//   - plan_question, a planner's question: the option it recommends, which
//     the work goes on with meanwhile (nothing reads the answer, and a
//     planner's question always recommends one);
//   - plan_change, plan_import and recorder_import, a proposal outside the
//     standing authorization: b, "Do not apply it" (any answer but a
//     refuses, plandecide.go);
//   - mission, a mission outside the standing envelope: b, "Do not start
//     it" (missiondecide.go).
const (
	CardMission        = "mission"
	CardPlanChange     = "plan_change"
	CardPlanImport     = "plan_import"
	CardRecorderImport = "recorder_import"
)

// cardOn is the card a question is in the runtime: its kind; its task, the
// task's step and the mission the card was asked in (a blocked card, a
// planner's question, a mission's card); and the stream a note of the rule's
// answer goes on: the mission's, or the proposal's (a plan's or a recorder
// log's) for a proposal's card.
type cardOn struct{ kind, task, step, mission, stream string }

// cardKind reads which card a question is, without locks: a blocked task's
// card (its owner_wait_opened), a proposal's (the plan_change_proposed that
// names it: a plan import, a recorder import or another change), a mission's
// (the move to awaiting_owner that names it), or a planner's question (a card
// naming a planning task); ok is false for anything else.
func cardKind(ctx context.Context, tx pgx.Tx, card RuntimeCard) (cardOn, bool, error) {
	var on cardOn
	err := tx.QueryRow(ctx, `SELECT o.task_id, COALESCE(t.plan_node_id, ''), o.mission_id FROM agentrt.event o
	  JOIN agentrt.task t ON t.task_id = o.task_id
	  WHERE o.event_type = 'owner_wait_opened' AND o.body_json->>'question_id' = $1`, card.ID).Scan(&on.task, &on.step, &on.mission)
	if err == nil {
		on.kind, on.stream = CardBlocked, MissionStream(on.mission)
		return on, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return on, false, err
	}
	var doc string
	err = tx.QueryRow(ctx, `SELECT event_stream_id, (body_json->'changes')::text FROM agentrt.event
	  WHERE event_type = 'plan_change_proposed' AND body_json->>'question_id' = $1 ORDER BY event_id LIMIT 1`, card.ID).Scan(&on.stream, &doc)
	if err == nil {
		var changes []PlanChange
		if jerr := json.Unmarshal([]byte(doc), &changes); jerr != nil {
			return on, false, fmt.Errorf("the proposal of question %s: %w", card.ID, jerr)
		}
		_, isImport := importOf(changes)
		_, isRecorder := recorderImportOf(changes)
		switch {
		case isImport:
			on.kind = CardPlanImport
		case isRecorder:
			on.kind = CardRecorderImport
		default:
			on.kind = CardPlanChange
		}
		return on, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return on, false, err
	}
	err = tx.QueryRow(ctx, `SELECT mission_id FROM agentrt.event WHERE event_type = 'mission_state_changed'
	  AND body_json->>'to' = 'awaiting_owner' AND body_json->>'question_id' = $1 ORDER BY event_id LIMIT 1`, card.ID).Scan(&on.mission)
	if err == nil {
		on.kind, on.stream = CardMission, MissionStream(on.mission)
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
	on.kind, on.task, on.stream = CardPlanQuestion, card.TaskID, MissionStream(on.mission)
	return on, err == nil, err
}

func ignoreNoRows(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

// ruleAnswers says whether a card-auto-answer rule in force answers cards of
// kind, and the card's mission ("" for a proposal's card) does not need the
// Owner: the card is then asked ByRule (TD-393, the coordinating thread
// 2026-10-20: the runtime's cards were held by the question store's quotas
// while no card was open for the rule to answer).
func ruleAnswers(ctx context.Context, tx pgx.Tx, kind, mission string) (bool, error) {
	r, err := inForce(ctx, tx, rules.KindCardAuto)
	if err != nil || r == nil || !slices.Contains(r.CardAuto.Kinds, kind) {
		return false, err
	}
	if mission == "" {
		return true, nil
	}
	var needsOwner string
	err = tx.QueryRow(ctx, `SELECT COALESCE(scope_json->>'needs_owner', '') FROM agentrt.mission WHERE mission_id = $1`, mission).Scan(&needsOwner)
	return err == nil && needsOwner == "", ignoreNoRows(err)
}

// inaction is the option of a card of kind that changes nothing; rec is the
// option the card recommends. It is known by its key, never by its label: on
// the five decision cards, which the platform asks with it second, the key
// the question store gives the second option (CardRefuseKey, b: do not do
// the task, do not apply the change, do not import, do not start the
// mission); on a planner's question, the option the card recommends, which
// the work goes on with. A planner's question that recommends nothing has
// none ("").
func inaction(kind, rec string) string {
	if kind == CardPlanQuestion {
		return rec
	}
	return CardRefuseKey
}

// ruleChoice is the option a rule answers a card of kind with, given the
// option the card recommends (rec, "" for none) and its AI assessment
// (TD-393; the coordinating thread 2026-10-20 for the Owner, 「STG 不等评
// 估」): a card is answered when its wait passes, whatever the assessment's
// state then.
//   - The card recommends an option: that option, when the assessment agrees
//     or has not come (none asked, one under way, one that cannot come);
//     when an assessment that has come recommends another, the option that
//     changes nothing.
//   - The card recommends nothing: the assessment's option when one has come
//     with it; otherwise the option that changes nothing.
//
// Under a rule that says assessment: veto (veto; review 489 A1 on T12, the
// coordinating thread 2026-10-22 for the Owner: A1) the assessment only ever
// keeps the rule from the card's recommendation:
//   - The card recommends an option: as above.
//   - The card recommends nothing: the option that changes nothing, whatever
//     the assessment says.
//   - The option so chosen is not one of the card's (options: a planner's
//     question that recommends nothing has no option that changes nothing):
//     no option (""), and the card is left to the Owner.
//
// why is what the card's answer says of an option the card does not
// recommend ("" for the one it does); said is what the note adds: which case
// it was.
func ruleChoice(kind, rec string, st assessState, now time.Time, veto bool, options []string) (option, why, said string) {
	if veto && rec == "" {
		option = inaction(kind, rec)
		why = "the card recommends nothing: the rule takes the option that changes nothing"
		switch {
		case st.done && st.recommended != "" && st.recommended == option:
			// Review 507 C1: what the assessment recommends is that option.
			why += ", which its AI assessment recommends too"
			said = why
		case st.done && st.recommended != "":
			why += ", not the one its AI assessment recommends (" + st.recommended + ")"
			said = why
		default:
			said = why + "; " + assessmentSaid(st, now)
		}
	} else {
		option, why, said = choiceOf(kind, rec, st, now)
	}
	if veto && !slices.Contains(options, option) {
		return "", "", ""
	}
	return option, why, said
}

// choiceOf is ruleChoice of a rule without assessment: veto (versions 1 to
// 3 read so).
func choiceOf(kind, rec string, st assessState, now time.Time) (option, why, said string) {
	nothing := inaction(kind, rec)
	ai := st.done && st.recommended != ""
	switch {
	case ai && rec != "" && st.recommended == rec:
		return rec, "", "its AI assessment agrees"
	case ai && rec == nothing:
		// What the card recommends is what changes nothing (a planner's
		// question: the option the work goes on with).
		return rec, "", fmt.Sprintf("its AI assessment recommends %s; the card's own option is the one that changes nothing", st.recommended)
	case ai && rec != "":
		why = fmt.Sprintf("the card recommends %s and its AI assessment %s: the rule takes the option that changes nothing", rec, st.recommended)
		return nothing, why, why
	case ai:
		why = fmt.Sprintf("the card recommends nothing and its AI assessment recommends %s: the rule takes the assessment's", st.recommended)
		return st.recommended, why, why
	case rec != "":
		return rec, "", assessmentSaid(st, now)
	}
	why = "the card recommends nothing and no AI assessment has come: the rule takes the option that changes nothing"
	return nothing, why, why + "; " + assessmentSaid(st, now)
}

// assessmentSaid is what a note says of an assessment that has not come.
func assessmentSaid(st assessState, now time.Time) string {
	switch {
	case !st.asked:
		return "no AI assessment was asked"
	case st.done:
		return "its AI assessment is unavailable (" + st.unavailable + ")"
	case st.late(now):
		return "no AI assessment came within " + timeoutText()
	}
	return "its AI assessment has not come yet"
}
