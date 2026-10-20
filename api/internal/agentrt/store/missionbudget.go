package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// A mission's budget as its tasks spend it (proposal §2, T10), and what the
// Owner's "one more round" adds to it (TD-384; PROD M-9, 2026-10-19: the
// Owner gave a blocked task one more round when the mission had used its 20
// attempts; the task went back to ready, every claim skipped it as over
// budget, and nothing said so).
//
//   - A claim leases a task only while its mission has an attempt and time
//     left (spend.why, claim.go).
//   - The controller's pass blocks a ready task its mission's budget will not
//     let run, with the reason (BlockOverBudget); the task then gets the
//     blocked card (blockedcard.go), a brief an answer to its sender
//     (brief.go).
//   - "Give it one more round" on a blocked card makes room in the budget for
//     one round (roundGrant): the attempts a round needs and the hours they
//     may run, recorded as mission_amended by the Owner, the card named in its
//     reason. While the budget has room nothing is added.
//
// What the Owner adds is kept apart in the budget (owner_granted_attempts,
// owner_granted_wall_hours), so the standing envelope a mission started under
// bounds the budget it started with, not what he added on a card: without
// that, his answer would raise the budget over the envelope's and the task
// would wait in ready again (allows). The envelope's hosts, executors and
// highest action still hold.

// budgetBlockSuffix ends the reason of a task blocked for its mission's
// budget (a card's recommendation tells such a block by it).
const budgetBlockSuffix = ": no host may take this task until the budget grows"

// roundAttempts is what one more round of a task needs: its implementation,
// the deterministic verification and the review (v5 §5.2 验收编排).
const roundAttempts = 3

// budgetDoc is a mission's budget_json as the runtime reads it.
type budgetDoc struct {
	MaxAttempts   int `json:"max_attempts"`
	MaxWallHours  int `json:"max_wall_hours"`
	OwnerAttempts int `json:"owner_granted_attempts"`
	OwnerHours    int `json:"owner_granted_wall_hours"`
}

// spend is a mission's budget and what it has used.
type spend struct {
	mission string
	budget  budgetDoc // its own, or its tenant rule's when it states none
	used    int       // attempts of all its tasks
	hours   float64   // since it was created; hours, not a Duration, which a budget of a million hours would overflow (review 428 C1)
	limitS  int       // the tenant rule's hard limit on an attempt
}

// spend reads m's budget; ok is false when there is none to spend: no budget
// rule of its tenant in force, or a budget that does not read or states no
// attempts or no hours. Nothing of such a mission is dispatched, and nothing
// is blocked or granted for it, since no answer on a card would change that.
func (m missionBudget) spend(ctx context.Context, tx pgx.Tx, rc ruleCache, mission string) (spend, bool, error) {
	rule, err := rc.get(ctx, tx, rules.KindBudget+":"+m.tenant)
	if err != nil || rule == nil {
		return spend{}, false, err
	}
	// A mission that states no budget has the rule's, as a proposal that
	// states none is given it.
	var b budgetDoc
	if json.Unmarshal(m.budget, &b) != nil {
		return spend{}, false, nil
	}
	if b.MaxAttempts == 0 && b.MaxWallHours == 0 {
		b.MaxAttempts, b.MaxWallHours = rule.Budget.Mission.MaxAttempts, rule.Budget.Mission.MaxWallHours
	}
	if b.MaxAttempts <= 0 || b.MaxWallHours <= 0 {
		return spend{}, false, nil
	}
	s := spend{mission: mission, budget: b, limitS: rule.Budget.AttemptLimitSec}
	var secs float64
	if err = tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM agentrt.attempt a JOIN agentrt.task t ON t.task_id = a.task_id WHERE t.mission_id = $1),
	    EXTRACT(EPOCH FROM clock_timestamp() - $2::timestamptz)::float8`, mission, m.created).Scan(&s.used, &secs); err != nil {
		return spend{}, false, err
	}
	s.hours = secs / 3600
	return s, true, nil
}

// why says why the budget lets no task of the mission run now, "" when it
// lets one run.
func (s spend) why() string {
	switch {
	case s.used >= s.budget.MaxAttempts:
		return fmt.Sprintf("mission %s has used %d of its %d attempts", s.mission, s.used, s.budget.MaxAttempts)
	case s.hours >= float64(s.budget.MaxWallHours):
		return fmt.Sprintf("mission %s is past its %d hours (it started %.1f hours ago)", s.mission, s.budget.MaxWallHours, s.hours)
	}
	return ""
}

// grant is what one more round adds to a mission's budget.
type grant struct{ attempts, hours int }

// roundGrant is what the budget needs for one more round of a task whose
// attempts may run taskLimitS each: roundAttempts more attempts than it has
// used, and hours for those attempts to run one after the other. Each may run
// the longer of the task's limit and a check's (a verification and a review
// have the default, review 428 B1), capped by the rule's limit, as a claim
// caps it. Zero when it has room.
func (s spend) roundGrant(taskLimitS int) grant {
	var g grant
	if need := s.used + roundAttempts; need > s.budget.MaxAttempts {
		g.attempts = need - s.budget.MaxAttempts
	}
	limit := max(taskLimitS, defaultHardLimitS)
	if s.limitS > 0 {
		limit = min(limit, s.limitS)
	}
	need := s.hours + float64(roundAttempts*limit)/3600
	if hours := int(math.Ceil(need)); hours > s.budget.MaxWallHours {
		g.hours = hours - s.budget.MaxWallHours
	}
	return g
}

// says is what a card tells the Owner one more round adds, "" for nothing.
func (s spend) says(g grant) string {
	if g == (grant{}) {
		return ""
	}
	text := fmt.Sprintf("One round needs up to %d attempts (implementation, verification, review); mission %s has used %d of its %d attempts",
		roundAttempts, s.mission, s.used, s.budget.MaxAttempts)
	if g.hours > 0 {
		text += fmt.Sprintf(" and %.1f of its %d hours", s.hours, s.budget.MaxWallHours)
	}
	text += ", so this answer also raises its budget to "
	switch {
	case g.attempts > 0 && g.hours > 0:
		text += fmt.Sprintf("%d attempts and %d hours", s.budget.MaxAttempts+g.attempts, s.budget.MaxWallHours+g.hours)
	case g.attempts > 0:
		text += fmt.Sprintf("%d attempts", s.budget.MaxAttempts+g.attempts)
	default:
		text += fmt.Sprintf("%d hours", s.budget.MaxWallHours+g.hours)
	}
	return text + ", recorded with this card's answer."
}

// readMissionBudget reads a mission as a budget check needs it, with the
// given lock clause ("" for none).
func readMissionBudget(ctx context.Context, tx pgx.Tx, mission, lock string) (missionBudget, error) {
	var m missionBudget
	err := tx.QueryRow(ctx, `SELECT state, tenant, budget_json, created_at, COALESCE(envelope_rule_id, '') FROM agentrt.mission
	  WHERE mission_id = $1 `+lock, mission).Scan(&m.state, &m.tenant, &m.budget, &m.created, &m.envelope)
	return m, err
}

// roundFor is the mission's spend and what one more round of task t needs,
// read in the caller's transaction (which holds t and its mission). ok is
// false when the mission has no budget to spend (spend).
func roundFor(ctx context.Context, tx pgx.Tx, t taskRow) (spend, grant, bool, error) {
	m, err := readMissionBudget(ctx, tx, t.mission, "")
	if err != nil {
		return spend{}, grant{}, false, err
	}
	var limitS int
	if err = tx.QueryRow(ctx, `SELECT hard_limit_s FROM agentrt.task WHERE task_id = $1`, t.id).Scan(&limitS); err != nil {
		return spend{}, grant{}, false, err
	}
	s, ok, err := m.spend(ctx, tx, ruleCache{}, t.mission)
	if err != nil || !ok {
		return spend{}, grant{}, false, err
	}
	return s, s.roundGrant(limitS), true, nil
}

// grantRound makes room in the mission's budget for one more round of task
// t, which its card's answer gave it (question): mission_amended by the
// Owner, or by the controller for the rule that answered for him (review 442
// A3), and the budget raised, its own figures written out (a mission that
// stated none had the rule's) with what the card added kept apart. The caller
// holds t and its mission's row FOR NO KEY UPDATE.
func grantRound(ctx context.Context, tx pgx.Tx, t taskRow, question string, by answerer) error {
	s, g, ok, err := roundFor(ctx, tx, t)
	if err != nil || !ok || g == (grant{}) {
		return err
	}
	b := s.budget
	set := map[string]any{"max_attempts": b.MaxAttempts + g.attempts, "max_wall_hours": b.MaxWallHours + g.hours}
	body := map[string]any{"reason": fmt.Sprintf("%s gave task %s one more round on its card (question %s); %s",
		by.who, t.id, question, s.added(g))}
	if g.attempts > 0 {
		set["owner_granted_attempts"] = b.OwnerAttempts + g.attempts
		body["max_attempts"] = b.MaxAttempts + g.attempts
	}
	if g.hours > 0 {
		set["owner_granted_wall_hours"] = b.OwnerHours + g.hours
		body["max_wall_hours"] = b.MaxWallHours + g.hours
	}
	doc, err := json.Marshal(set)
	if err != nil {
		return err
	}
	if _, err = Append(ctx, tx, Event{Stream: MissionStream(t.mission), Type: "mission_amended", Actor: by.actor,
		MissionID: t.mission, PlanNode: t.step, TaskID: t.id, Body: body}); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE agentrt.mission SET budget_json = budget_json || $2::jsonb, version = version + 1 WHERE mission_id = $1`,
		t.mission, string(doc))
	return err
}

// added says what a grant adds, for mission_amended's reason.
func (s spend) added(g grant) string {
	text := fmt.Sprintf("it had used %d of %d attempts", s.used, s.budget.MaxAttempts)
	if g.hours > 0 {
		text += fmt.Sprintf(" and %.1f of %d hours", s.hours, s.budget.MaxWallHours)
	}
	text += "; a round needs up to " + fmt.Sprint(roundAttempts) + " attempts"
	if g.attempts > 0 {
		text += fmt.Sprintf(": %d attempts added", g.attempts)
	}
	if g.hours > 0 {
		text += fmt.Sprintf(", %d hours added", g.hours)
	}
	return text
}

// BlockOverBudget is one pass blocking every ready task whose mission is
// active and whose budget lets no task run now (its attempts used, or its
// hours over), with the reason, so the task gets its card (TD-384). It
// returns how many tasks it blocked. One that cannot be blocked now does not
// hold up the others; the next pass tries it again.
func BlockOverBudget(ctx context.Context, db DB) (int, error) {
	n := 0
	var errs []error
	after := ""
	page := int(blockedPage.Load())
	for {
		var missions []string
		if err := InTx(ctx, db, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `SELECT DISTINCT t.mission_id FROM agentrt.task t JOIN agentrt.mission m ON m.mission_id = t.mission_id
			  WHERE t.state = 'ready' AND m.state = 'active' AND t.mission_id > $1 ORDER BY t.mission_id LIMIT $2`, after, page)
			if err != nil {
				return err
			}
			missions, err = pgx.CollectRows(rows, pgx.RowTo[string])
			return err
		}); err != nil {
			return n, errors.Join(append(errs, err)...)
		}
		for _, id := range missions {
			k, err := blockOverBudget(ctx, db, id)
			n += k
			if err != nil {
				errs = append(errs, fmt.Errorf("mission %s: %w", id, err))
			}
		}
		if len(missions) < page {
			return n, errors.Join(errs...)
		}
		after = missions[len(missions)-1]
	}
}

// blockOverBudget blocks the ready tasks of one mission whose budget is
// spent. A read without locks finds them (most missions have room, and their
// tasks are not locked); each is then blocked under its lock and its
// mission's, shared, read again: a claim of the mission takes that row
// exclusively to count its attempts, so a claim either leases first and is
// counted or waits for this.
func blockOverBudget(ctx context.Context, db DB, mission string) (int, error) {
	var tasks []string
	if err := InTx(ctx, db, func(tx pgx.Tx) error {
		m, err := readMissionBudget(ctx, tx, mission, "")
		if err != nil {
			return err
		}
		s, ok, err := m.spend(ctx, tx, ruleCache{}, mission)
		if err != nil || !ok || m.state != "active" || s.why() == "" {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT task_id FROM agentrt.task WHERE mission_id = $1 AND state = 'ready' ORDER BY task_id`, mission)
		if err != nil {
			return err
		}
		tasks, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	}); err != nil {
		return 0, err
	}
	n := 0
	var errs []error
	for _, id := range tasks {
		did := false
		err := InTx(ctx, db, func(tx pgx.Tx) error {
			did = false
			t, err := readTask(ctx, tx, id, true)
			if err != nil || t.state != "ready" {
				return err
			}
			m, err := readMissionBudget(ctx, tx, t.mission, "FOR SHARE")
			if err != nil || m.state != "active" {
				return err
			}
			s, ok, err := m.spend(ctx, tx, ruleCache{}, t.mission)
			if err != nil || !ok || s.why() == "" {
				return err
			}
			did = true
			return moveTask(ctx, tx, "controller", t, "blocked", s.why()+budgetBlockSuffix)
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("task %s: %w", id, err))
		} else if did {
			n++
		}
	}
	return n, errors.Join(errs...)
}
