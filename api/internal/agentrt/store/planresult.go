package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seatproto"
)

// Using a plan (W-31 2.2b, the thin chief of staff, slice 3). The acceptance
// pass reads a submitted plan (planparse.go) and, when it passes, records it
// in one transaction, in this order:
//
//   - the Owner's decision cards, each with the option the work goes on with
//     while he has not answered (the question store, keyed by the task, its
//     round and the card's place, so a retried pass finds the same cards);
//   - the mission amended: its criteria, what is out of scope, a budget no
//     larger than it had (mission_amended);
//   - the implementation tasks, each with the repository's checks and the
//     plan's own, on the commit the planner read (task_proposed);
//   - the choices the planner made, for the Owner to audit (decision_recorded,
//     the Owner's D2 A);
//   - the planning task accepted.
//
// The seat writes what the plan decides; the controller moves the task. A
// plan that does not pass goes back for another round with the reason added
// to its spec, as an implementation goes back for rework, until its rounds
// run out (max_rounds, 2 by default); then the task is blocked with the
// reason (review 216 A3). A mission that is no longer open blocks it at once.
// Nothing else is written but the plan's policy alerts (alerts.go), and the
// cards a refused round made are withdrawn in the same pass, before the
// refusal is written (review 217 B1).
//
// Lock order: the planning task's attempt, the task, the mission's row, the
// seat as a writer, the dependency graph and steps (lockBatch), then the
// streams. The graph lock comes before any event, as ProposeTasks over HTTP
// takes it before its own (review 216 A2).

// Seat is the actor a plan's records are written as.
const Seat = "seat:chief_of_staff"

// Withdraw withdraws a card the runtime asked, with a reason; a card that is
// already answered, withdrawn or gone is not an error.
type Withdraw func(ctx context.Context, questionID, reason string) error

// ErrAsked: the question store holds this question already, open or
// answered, under another key (a card of an earlier round, or another plan's).
// The Owner has been asked; a plan goes on with its default and does not
// count the card as its own (review 217 B2).
var ErrAsked = errors.New("store: the Owner was already asked this question")

// PendingPlans lists the planning tasks with a submitted plan.
func PendingPlans(ctx context.Context, db DB) ([]string, error) {
	var ids []string
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT task_id FROM agentrt.task WHERE state = 'submitted' AND role = 'plan' ORDER BY updated_at, task_id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return rows.Err()
	})
	return ids, err
}

// AcceptPlan uses the plan a planning task handed in. It returns "accepted",
// "rework" (another round, the reason in its spec), "blocked" (no round left,
// or the mission is closed; the reason is on the task) or "" when there was
// nothing to do. An error leaves everything as it was; cards the question
// store already holds are found again by their keys on the next pass.
func AcceptPlan(ctx context.Context, db DB, taskID string, ask Ask, withdraw Withdraw) (string, error) {
	var did string
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		did = ""
		_, conclusion, report, _, err := submittedAttempt(ctx, tx, taskID, "FOR UPDATE")
		if errors.Is(err, errNoSubmitted) {
			return nil
		}
		if err != nil {
			return err
		}
		t, err := readTask(ctx, tx, taskID, true)
		if err != nil || t.role != "plan" || t.state != "submitted" {
			return err
		}
		d, err := readDetail(ctx, tx, taskID)
		if err != nil {
			return err
		}
		var state string
		var repos []string
		var scope, budget map[string]any
		if err = tx.QueryRow(ctx, `SELECT state, COALESCE(ARRAY(SELECT jsonb_array_elements_text(scope_json->'repos')), '{}'),
		  scope_json, budget_json FROM agentrt.mission WHERE mission_id = $1 FOR NO KEY UPDATE`,
			t.mission).Scan(&state, &repos, &scope, &budget); err != nil {
			return err
		}
		// The planner's POLICY_ALERT lines are recorded and told to the Owner
		// whether the plan is used or refused (a refused round still saw what
		// it reports; the next round's same lines are not told twice:
		// alerts.go): on refusal before the refusal is written, on use once
		// the dependency graph is locked, so no stream is written before the
		// graph lock a batch of tasks takes first (review 370 A1). The seat's
		// lock first, as below.
		var alerts []string
		noteAlerts := func() error {
			if len(alerts) == 0 {
				return nil
			}
			if e := ensureWriter(ctx, tx, Writer{Actor: Seat}, false); e != nil {
				return e
			}
			return tellOwnerOfAlerts(ctx, tx, t.mission, taskID, "", "planning mission "+t.mission, alerts)
		}
		refuse := func(why string) error {
			if e := noteAlerts(); e != nil {
				return e
			}
			var e error
			did, e = refusePlan(ctx, tx, t, d, why)
			return e
		}
		switch {
		case !slices.Contains([]string{"proposed", "awaiting_owner", "active", "paused"}, state):
			did = "blocked"
			return moveTask(ctx, tx, "controller", t, "blocked", "plan not used: the mission is "+state)
		case conclusion != "done":
			return refuse(fmt.Sprintf("the planner's attempt ended %q", conclusion))
		case report == "":
			return refuse("the planner's attempt has no report")
		}
		var body []byte
		if err = tx.QueryRow(ctx, `SELECT body FROM agentrt.artifact_blob WHERE artifact_id = $1`, report).Scan(&body); err != nil {
			return err
		}
		alerts = seatproto.Alerts(strings.ToValidUTF8(string(body), "\uFFFD"))
		known := slices.DeleteFunc(slices.Clone(repos), func(r string) bool { _, ok := Repos[r]; return !ok })
		p, err := ParsePlan(body, known)
		if err != nil {
			return refuse(err.Error())
		}
		maxAttempts, maxHours := budgetInt(budget, "max_attempts"), budgetInt(budget, "max_wall_hours")
		if (p.Mission.MaxAttempts > 0 && p.Mission.MaxAttempts > maxAttempts) || (p.Mission.MaxWallHours > 0 && p.Mission.MaxWallHours > maxHours) {
			return refuse(fmt.Sprintf("the plan raises the mission's budget (%d attempts, %d hours); a plan may only lower it", maxAttempts, maxHours))
		}
		// Each task starts from its repository's main as the goal read it
		// (scope_json.bases); the planning task's own repository from the
		// planning task's base, as before goals kept bases. A repository with
		// neither has no commit to start from: never another repository's.
		bases := map[string]string{}
		if b, ok := scope["bases"].(map[string]any); ok {
			for r, v := range b {
				if c, ok := v.(string); ok {
					bases[r] = c
				}
			}
		}
		baseOf := func(repo string) string {
			if b := bases[repo]; b != "" {
				return b
			}
			if repo == d.repo {
				return d.base
			}
			return ""
		}
		for i, pt := range p.Tasks {
			if baseOf(pt.Repo) == "" {
				return refuse(fmt.Sprintf("tasks[%d]: the goal recorded no commit of %s to start from", i, pt.Repo))
			}
		}
		var made, asked []string // cards this round made; questions asked before
		byRule := false
		if len(p.Questions) > 0 {
			if byRule, err = ruleAnswers(ctx, tx, CardPlanQuestion, t.mission); err != nil {
				return err
			}
		}
		for i, q := range p.Questions {
			if ask == nil {
				return fmt.Errorf("%w: no question store is configured", ErrAsk)
			}
			id, err := ask(ctx, Card{Key: planCardKey(taskID, d.round, i), Title: q.Question, Body: q.Body, Options: q.Options,
				Recommended: q.Recommended, Meanwhile: q.Meanwhile, OwnerDuty: q.OwnerDuty, TaskID: taskID, ByRule: byRule})
			switch {
			case errors.Is(err, ErrAsked):
				asked = append(asked, strings.TrimPrefix(err.Error(), ErrAsked.Error()+": "))
				continue
			case errors.Is(err, ErrInvalid):
				// The cards this round made claim work that will not go on as
				// they say: withdrawn here, so a withdrawal that fails leaves the
				// plan submitted and the next pass does it all again (review
				// 217 B1).
				for _, m := range made {
					if withdraw == nil {
						break
					}
					if werr := withdraw(ctx, m, "the plan that asked this was not used; see its task "+taskID); werr != nil {
						return fmt.Errorf("%w: withdraw %s: %v", ErrAsk, m, werr)
					}
				}
				return refuse(fmt.Sprintf("questions[%d]: %v", i, err))
			case err != nil:
				return fmt.Errorf("%w: %v", ErrAsk, err)
			}
			made = append(made, id)
		}
		// The seat's lock before the implementation specs are stored: a
		// message to the seat stores its body under the sender's lock and the
		// seat's mail is read under the seat's, then the sender's (review 270
		// B1).
		seat := Writer{Actor: Seat}
		if err = ensureWriter(ctx, tx, seat, false); err != nil {
			return err
		}
		tasks := make([]TaskProposal, 0, len(p.Tasks))
		for _, pt := range p.Tasks {
			verify := slices.Clone(Repos[pt.Repo].Verify)
			for _, c := range pt.Verify {
				if !slices.ContainsFunc(verify, func(v []string) bool { return slices.Equal(v, c) }) {
					verify = append(verify, c)
				}
			}
			spec, err := ImplementSpec(pt.Title, pt.Text, Repos[pt.Repo], verify)
			if err != nil {
				return err
			}
			id, err := PutArtifact(ctx, tx, Seat, "task_spec", "application/json", spec)
			if err != nil {
				return err
			}
			tasks = append(tasks, TaskProposal{Ref: pt.Ref, OffPlanReason: "planned by task " + taskID, Role: "implement",
				Class: "goal", Title: pt.Title, SpecArtifact: id, Effect: "branch", Repo: pt.Repo, BaseCommit: baseOf(pt.Repo),
				Core: pt.IsCore(), DependsOn: pt.DependsOn})
		}
		if err = checkBatch(tasks); err != nil {
			return refuse(err.Error())
		}
		if err = lockBatch(ctx, tx, tasks); err != nil {
			return err
		}
		if err = noteAlerts(); err != nil {
			return err
		}
		if err = amendMission(ctx, tx, t.mission, taskID, p.Mission); err != nil {
			return err
		}
		if _, err = proposeTasksTx(ctx, tx, seat, t.mission, tasks); err != nil {
			return err
		}
		for _, c := range p.Decisions {
			body := map[string]any{"decision": c.Decision, "why": c.Why}
			if c.Dropped != "" {
				body["dropped"] = c.Dropped
			}
			if c.Reverse != "" {
				body["reverse"] = c.Reverse
			}
			if _, err = Append(ctx, tx, Event{Stream: MissionStream(t.mission), Type: "decision_recorded", Actor: Seat,
				MissionID: t.mission, TaskID: taskID, Body: body}); err != nil {
				return err
			}
		}
		did = "accepted"
		reason := fmt.Sprintf("plan used: %d tasks, %d decisions", len(p.Tasks), len(p.Decisions))
		if len(made) > 0 {
			reason += ", questions " + strings.Join(made, ", ")
		}
		if len(asked) > 0 {
			reason += ", asked before: " + strings.Join(asked, "; ")
		}
		return moveTask(ctx, tx, "controller", t, "accepted", reason)
	})
	return did, err
}

// refusePlan sends a plan that did not pass back for another round, with the
// reason added to its spec, or blocks it when its rounds have run out.
func refusePlan(ctx context.Context, tx pgx.Tx, t taskRow, d taskDetail, why string) (string, error) {
	why = oneLineRunes("plan not used: "+why, 500)
	if d.round+1 > d.maxRounds {
		return "blocked", failWork(ctx, tx, "controller", t, fmt.Sprintf("%s (round %d of %d)", why, d.round, d.maxRounds))
	}
	spec, err := reworkSpec(ctx, tx, d.spec, d.round+1, why)
	if err != nil {
		return "", err
	}
	if err = moveTask(ctx, tx, "controller", t, "verifying", "checking the plan"); err != nil {
		return "", err
	}
	t.state = "verifying"
	if err = moveTask(ctx, tx, "controller", t, "rework", fmt.Sprintf("%s (round %d of %d)", why, d.round, d.maxRounds)); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `UPDATE agentrt.task SET round_no = round_no + 1, spec_artifact_id = $2 WHERE task_id = $1`, t.id, spec); err != nil {
		return "", err
	}
	t.state = "rework"
	return "rework", moveTask(ctx, tx, "controller", t, "ready", fmt.Sprintf("round %d, spec %s", d.round+1, spec))
}

// reworkSpec is a planning task's spec for its next round: the spec it had,
// and why the plan it handed in was not used.
func reworkSpec(ctx context.Context, tx pgx.Tx, specID string, round int, why string) (string, error) {
	var body []byte
	if err := tx.QueryRow(ctx, `SELECT body FROM agentrt.artifact_blob WHERE artifact_id = $1`, specID).Scan(&body); err != nil {
		return "", err
	}
	// A spec is canonical JSON with its text, or plain markdown that is all
	// text (agentd's ParseSpec).
	doc := map[string]any{"text": string(body)}
	if trimmed := bytes.TrimSpace(body); len(trimmed) > 0 && trimmed[0] == '{' {
		doc = nil
		if err := json.Unmarshal(body, &doc); err != nil {
			return "", fmt.Errorf("store: the planning spec %s does not read: %w", specID, err)
		}
	}
	text, _ := doc["text"].(string)
	// why is worded as refusePlan words it: one line, "plan not used: " and
	// the reason, cut; ReworkText words the reason the same way again, which
	// leaves it as it is.
	doc["text"] = ReworkText(text, round, strings.TrimPrefix(why, "plan not used: "))
	b, err := canon.Encode(doc)
	if err != nil {
		return "", err
	}
	return PutArtifact(ctx, tx, "controller", "task_spec", "application/json", b)
}

// ReworkText is the text of a planning spec's next round: the text it had,
// and why, the reason the plan handed in before was not used (as the check
// that refused it gives it: ParsePlan's error), on one line and cut as
// refusePlan words it. agentd exam gives a refused plan its second round
// with it, as the controller would (the chief exam's cut §3.1, step 3).
func ReworkText(text string, round int, why string) string {
	why = oneLineRunes("plan not used: "+why, 500)
	return text + fmt.Sprintf("\n\n---\n## Round %d\n\nThe plan you handed in before was not used: %s\n\n"+
		"Hand in a plan that passes; everything above still holds.\n", round, strings.TrimPrefix(why, "plan not used: "))
}

// amendMission records what a plan says about its mission, when it says
// anything: the criteria replace the mission's, what is out of scope joins
// its scope, a budget the plan gives (never larger, checked before) replaces
// its own.
func amendMission(ctx context.Context, tx pgx.Tx, mission, taskID string, m PlannedMission) error {
	body := map[string]any{"reason": "planned by task " + taskID}
	sets := []string{}
	args := []any{mission}
	// put sets col to op followed by v: "" replaces it, "col || " merges v in.
	put := func(col, op string, v any) error {
		b, err := canon.Encode(v)
		if err != nil {
			return err
		}
		args = append(args, string(b))
		sets = append(sets, fmt.Sprintf("%s = %s$%d::jsonb", col, op, len(args)))
		return nil
	}
	if len(m.Success) > 0 {
		body["success"] = m.Success
		if err := put("success_json", "", map[string]any{"criteria": m.Success}); err != nil {
			return err
		}
	}
	if len(m.OutOfScope) > 0 {
		body["out_of_scope"] = m.OutOfScope
		if err := put("scope_json", "scope_json || ", map[string]any{"out_of_scope": m.OutOfScope}); err != nil {
			return err
		}
	}
	b := map[string]any{}
	if m.MaxAttempts > 0 {
		body["max_attempts"], b["max_attempts"] = m.MaxAttempts, m.MaxAttempts
	}
	if m.MaxWallHours > 0 {
		body["max_wall_hours"], b["max_wall_hours"] = m.MaxWallHours, m.MaxWallHours
	}
	if len(b) > 0 {
		if err := put("budget_json", "budget_json || ", b); err != nil {
			return err
		}
	}
	if len(sets) == 0 {
		return nil
	}
	if _, err := Append(ctx, tx, Event{Stream: MissionStream(mission), Type: "mission_amended", Actor: Seat,
		MissionID: mission, TaskID: taskID, Body: body}); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE agentrt.mission SET `+strings.Join(sets, ", ")+`, version = version + 1 WHERE mission_id = $1`, args...)
	return err
}

func budgetInt(budget map[string]any, key string) int {
	switch v := budget[key].(type) {
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	}
	return 0
}

// planCardKey is the key in the question store of the i-th card of a
// planning task's round: the same card on every pass over the same round.
func planCardKey(taskID string, round, i int) string {
	sum, _ := canon.Sum(map[string]any{"task": taskID, "round": round, "card": i, "route": "plans"})
	return strings.TrimPrefix(sum, "sha256:")
}
