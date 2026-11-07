package store

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// A mission's budget (proposal §2, T10) is read for display and for the
// tenant rule's hard limit on an attempt; its attempts and wall hours refuse
// nothing (the Owner, 2026-11-06: in 14 days the attempt and hour caps
// blocked working tasks 26 times, M-308 alone 21 times, and stopped no
// runaway; money has its own cap, the vendor pool budget, pools.go).
//
//   - A claim leases a task while its tenant's budget rule is in force
//     (envelope.go allows), however many attempts its mission has used and
//     however long ago it started.
//   - Nothing blocks a ready task for its mission's budget, and nothing grows
//     the budget, on a card or on its own: "Give it one more round" on a
//     blocked card needs no room in it.
//   - A task blocked for its budget before (its reason ends with
//     budgetBlockSuffix) keeps its blocked card until it is answered.
//
// What the Owner and the platform added before is kept apart in the budget
// (owner_granted_attempts, owner_granted_wall_hours, auto_granted_attempts,
// auto_granted_wall_hours), so the standing envelope a mission started under
// still bounds the budget it started with (allows).

// budgetBlockSuffix ended the reason of a task blocked for its mission's
// budget (a card's recommendation tells such a block by it).
const budgetBlockSuffix = ": no host may take this task until the budget grows"

// budgetDoc is a mission's budget_json as the runtime reads it.
type budgetDoc struct {
	MaxAttempts   int `json:"max_attempts"`
	MaxWallHours  int `json:"max_wall_hours"`
	OwnerAttempts int `json:"owner_granted_attempts"`
	OwnerHours    int `json:"owner_granted_wall_hours"`
	AutoAttempts  int `json:"auto_granted_attempts"`
	AutoHours     int `json:"auto_granted_wall_hours"`
}

// spend is a mission's budget as the runtime reads it.
type spend struct {
	mission string
	budget  budgetDoc // its own, or its tenant rule's when it states none
	limitS  int       // the tenant rule's hard limit on an attempt
}

// spend reads m's budget; ok is false when its tenant has no budget rule in
// force, and nothing of such a mission is dispatched, since there is no hard
// limit for its attempts. A budget that does not read, or states no attempts
// or hours, is read as the rule's.
func (m missionBudget) spend(ctx context.Context, tx pgx.Tx, rc ruleCache, mission string) (spend, bool, error) {
	rule, err := rc.get(ctx, tx, rules.KindBudget+":"+m.tenant)
	if err != nil || rule == nil {
		return spend{}, false, err
	}
	var b budgetDoc
	if json.Unmarshal(m.budget, &b) != nil {
		b = budgetDoc{}
	}
	if b.MaxAttempts == 0 && b.MaxWallHours == 0 {
		b.MaxAttempts, b.MaxWallHours = rule.Budget.Mission.MaxAttempts, rule.Budget.Mission.MaxWallHours
	}
	return spend{mission: mission, budget: b, limitS: rule.Budget.AttemptLimitSec}, true, nil
}

// readMissionBudget reads a mission as a budget check needs it, with the
// given lock clause ("" for none).
func readMissionBudget(ctx context.Context, tx pgx.Tx, mission, lock string) (missionBudget, error) {
	var m missionBudget
	err := tx.QueryRow(ctx, `SELECT state, tenant, budget_json, created_at, COALESCE(envelope_rule_id, '') FROM agentrt.mission
	  WHERE mission_id = $1 `+lock, mission).Scan(&m.state, &m.tenant, &m.budget, &m.created, &m.envelope)
	return m, err
}

// spendOf is the spend of task t's mission, read in the caller's
// transaction (which holds t and its mission). ok is false when the mission
// has no budget rule in force (spend).
func spendOf(ctx context.Context, tx pgx.Tx, t taskRow) (spend, bool, error) {
	m, err := readMissionBudget(ctx, tx, t.mission, "")
	if err != nil {
		return spend{}, false, err
	}
	return m.spend(ctx, tx, ruleCache{}, t.mission)
}
