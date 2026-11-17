package store

import (
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"
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
	kinds, err := cardAutoKindsInForce(ctx, tx)
	if err != nil || !slices.Contains(kinds, kind) {
		return false, err
	}
	if mission == "" {
		return true, nil
	}
	var needsOwner string
	err = tx.QueryRow(ctx, `SELECT COALESCE(scope_json->>'needs_owner', '') FROM agentrt.mission WHERE mission_id = $1`, mission).Scan(&needsOwner)
	return err == nil && needsOwner == "", ignoreNoRows(err)
}
