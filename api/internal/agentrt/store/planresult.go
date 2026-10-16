package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

// Plans (W-31 2.2b, the thin chief of staff, slice 3). A planning task hands
// in a report that ends in one fenced JSON block, in the format its spec gives
// (goal.go). The acceptance pass reads it and checks it by program (ADR §12.2
// item 13: the model proposes, the program lets it through); a plan that
// passes is recorded in one transaction, in this order:
//
//   - the Owner's decision cards, each with the option the work goes on with
//     while he has not answered (the question store, keyed by the task and
//     the card's place, so a retried pass finds the same cards);
//   - the mission amended: its criteria, what is out of scope, a budget no
//     larger than it had (mission_amended);
//   - the implementation tasks, each with the repository's checks and the
//     plan's own, on the commit the planner read (task_proposed);
//   - the choices the planner made, for the Owner to audit (decision_recorded,
//     the Owner's D2 A);
//   - the planning task accepted.
//
// The seat writes what the plan decides; the controller moves the task. A
// plan that does not pass blocks its task with the reason and writes nothing
// else; the Owner and the next planning session see why.

// Bounds of a plan.
const (
	PlanMaxTasks     = 8
	PlanMaxQuestions = 3
	PlanMaxDecisions = 10
	PlanMaxCriteria  = 10
	planMaxVerify    = 5    // checks a plan adds to a task
	planMaxText      = 8000 // runes of a task's text
	planMaxField     = 1000 // runes of a criterion or of a decision's field
	planMaxQuestion  = 300  // the question store's title
	planMaxBody      = 2500 // a card's body, under the question store's 3000
	planMaxLabel     = 160  // an option
	planMaxRef       = 40
)

// Seat is the actor a plan's records are written as.
const Seat = "seat:chief_of_staff"

// PlanDoc is a plan as a planner hands it in.
type PlanDoc struct {
	Mission   PlannedMission    `json:"mission"`
	Tasks     []PlannedTask     `json:"tasks"`
	Questions []PlannedQuestion `json:"questions"`
	Decisions []PlannedDecision `json:"decisions"`
}

// PlannedMission is what a plan says about its mission; empty keeps what the
// mission has.
type PlannedMission struct {
	Success      []string `json:"success"`
	OutOfScope   []string `json:"out_of_scope"`
	MaxAttempts  int      `json:"max_attempts"`
	MaxWallHours int      `json:"max_wall_hours"`
}

// PlannedTask is one implementation task of a plan.
type PlannedTask struct {
	Ref       string     `json:"ref"`
	Title     string     `json:"title"`
	Repo      string     `json:"repo"`
	Text      string     `json:"text"`
	Verify    [][]string `json:"verify"`
	DependsOn []string   `json:"depends_on"`
}

// PlannedQuestion is a decision card for the Owner.
type PlannedQuestion struct {
	Question    string   `json:"question"`
	Body        string   `json:"body"`
	Options     []string `json:"options"`
	Recommended int      `json:"recommended"`
	Meanwhile   string   `json:"meanwhile"`
	OwnerDuty   string   `json:"owner_duty"`
}

// PlannedDecision is a choice the planner made.
type PlannedDecision struct {
	Decision string `json:"decision"`
	Why      string `json:"why"`
	Dropped  string `json:"dropped"`
	Reverse  string `json:"reverse"`
}

var planFence = regexp.MustCompile("(?s)```json[ \\t]*\\r?\\n(.*?)\\r?\\n[ \\t]*```")

// ParsePlan reads the last fenced json block of a planner's report and checks
// it; repos are the repositories its mission names. An error says what is
// wrong in words a planner can act on.
func ParsePlan(report []byte, repos []string) (PlanDoc, error) {
	var d PlanDoc
	blocks := planFence.FindAllSubmatch(report, -1)
	if len(blocks) == 0 {
		return d, errors.New("the report has no ```json block")
	}
	dec := json.NewDecoder(bytes.NewReader(blocks[len(blocks)-1][1]))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return d, fmt.Errorf("the plan does not read: %v", err)
	}
	if dec.More() {
		return d, errors.New("the plan block holds more than one value")
	}
	return d, d.check(repos)
}

func (d PlanDoc) check(repos []string) error {
	m := d.Mission
	if err := texts("mission.success", m.Success, PlanMaxCriteria, planMaxField); err != nil {
		return err
	}
	if err := texts("mission.out_of_scope", m.OutOfScope, PlanMaxCriteria, planMaxField); err != nil {
		return err
	}
	if m.MaxAttempts < 0 || m.MaxWallHours < 0 {
		return errors.New("mission: a budget is not negative")
	}
	if len(d.Tasks) == 0 || len(d.Tasks) > PlanMaxTasks {
		return fmt.Errorf("a plan has 1 to %d tasks, not %d", PlanMaxTasks, len(d.Tasks))
	}
	refs := map[string]bool{}
	for i, t := range d.Tasks {
		at := fmt.Sprintf("tasks[%d]", i)
		switch {
		case t.Ref == "" || len(t.Ref) > planMaxRef || strings.IndexFunc(t.Ref, notRefRune) >= 0:
			return fmt.Errorf("%s: ref is 1 to %d letters, digits, - or _", at, planMaxRef)
		case refs[t.Ref]:
			return fmt.Errorf("%s: ref %q is used twice", at, t.Ref)
		case !slices.Contains(repos, t.Repo):
			return fmt.Errorf("%s: repo is one of %s, not %q", at, strings.Join(repos, ", "), t.Repo)
		case len(t.Verify) > planMaxVerify:
			return fmt.Errorf("%s: at most %d checks of its own", at, planMaxVerify)
		}
		refs[t.Ref] = true
		if err := text(at+".title", t.Title, maxTaskTitle, false); err != nil {
			return err
		}
		if err := text(at+".text", t.Text, planMaxText, true); err != nil {
			return err
		}
		for j, c := range t.Verify {
			if len(c) == 0 || c[0] != "go" {
				return fmt.Errorf("%s.verify[%d]: a check starts with go", at, j)
			}
			for _, a := range c {
				if err := text(fmt.Sprintf("%s.verify[%d]", at, j), a, 200, false); err != nil {
					return err
				}
			}
		}
	}
	for i, t := range d.Tasks {
		for _, on := range t.DependsOn {
			if !refs[on] || on == t.Ref {
				return fmt.Errorf("tasks[%d]: depends_on names %q, which is not another task of this plan", i, on)
			}
		}
	}
	if cyc := dependencyCycle(d.Tasks); cyc != "" {
		return fmt.Errorf("the tasks' depends_on make a cycle through %s", cyc)
	}
	if len(d.Questions) > PlanMaxQuestions {
		return fmt.Errorf("a plan asks the Owner at most %d questions", PlanMaxQuestions)
	}
	for i, q := range d.Questions {
		at := fmt.Sprintf("questions[%d]", i)
		if err := text(at+".question", q.Question, planMaxQuestion, false); err != nil {
			return err
		}
		if q.Body != "" {
			if err := text(at+".body", q.Body, planMaxBody, true); err != nil {
				return err
			}
		}
		if err := texts(at+".options", q.Options, 4, planMaxLabel); err != nil {
			return err
		}
		switch {
		case len(q.Options) < 2:
			return fmt.Errorf("%s: 2 to 4 options", at)
		case q.Recommended < 1 || q.Recommended > len(q.Options):
			return fmt.Errorf("%s: recommended is the 1-based option the work goes on with", at)
		case !slices.Contains([]string{"goal", "rule", "irreversible"}, q.OwnerDuty):
			return fmt.Errorf("%s: owner_duty is goal, rule or irreversible", at)
		}
		if err := text(at+".meanwhile", q.Meanwhile, planMaxQuestion, false); err != nil {
			return err
		}
	}
	if len(d.Decisions) > PlanMaxDecisions {
		return fmt.Errorf("a plan records at most %d decisions", PlanMaxDecisions)
	}
	for i, c := range d.Decisions {
		at := fmt.Sprintf("decisions[%d]", i)
		for name, v := range map[string]string{"decision": c.Decision, "why": c.Why} {
			if err := text(at+"."+name, v, planMaxField, true); err != nil {
				return err
			}
		}
		for name, v := range map[string]string{"dropped": c.Dropped, "reverse": c.Reverse} {
			if v != "" {
				if err := text(at+"."+name, v, planMaxField, true); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func notRefRune(r rune) bool {
	return !(r == '-' || r == '_' || r < utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r)))
}

// text checks one string: present, at most max runes, no control character
// but a line break or a tab where lines are allowed.
func text(at, s string, max int, lines bool) error {
	switch n := utf8.RuneCountInString(strings.TrimSpace(s)); {
	case n == 0:
		return fmt.Errorf("%s is empty", at)
	case utf8.RuneCountInString(s) > max:
		return fmt.Errorf("%s is longer than %d characters", at, max)
	}
	for _, c := range s {
		if unicode.IsControl(c) && !(lines && (c == '\n' || c == '\t')) {
			return fmt.Errorf("%s has a control character %U", at, c)
		}
	}
	return nil
}

func texts(at string, list []string, most, max int) error {
	if len(list) > most {
		return fmt.Errorf("%s holds at most %d", at, most)
	}
	for i, s := range list {
		if err := text(fmt.Sprintf("%s[%d]", at, i), s, max, false); err != nil {
			return err
		}
	}
	return nil
}

// dependencyCycle names a task on a cycle of depends_on, or "".
func dependencyCycle(tasks []PlannedTask) string {
	deps := map[string][]string{}
	for _, t := range tasks {
		deps[t.Ref] = t.DependsOn
	}
	state := map[string]int{} // 1 on the path, 2 done
	var visit func(string) string
	visit = func(ref string) string {
		switch state[ref] {
		case 1:
			return ref
		case 2:
			return ""
		}
		state[ref] = 1
		for _, on := range deps[ref] {
			if c := visit(on); c != "" {
				return c
			}
		}
		state[ref] = 2
		return ""
	}
	for _, t := range tasks {
		if c := visit(t.Ref); c != "" {
			return c
		}
	}
	return ""
}

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
// "blocked" (the plan did not pass; the reason is on the task) or "" when
// there was nothing to do. An error leaves everything as it was, cards the
// question store already holds aside: the next pass finds them by their keys.
func AcceptPlan(ctx context.Context, db DB, taskID string, ask Ask) (string, error) {
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
		block := func(why string) error {
			did = "blocked"
			return moveTask(ctx, tx, "controller", t, "blocked", oneLineRunes("plan not used: "+why, 500))
		}
		var state string
		var repos []string
		var success, scope, budget map[string]any
		if err = tx.QueryRow(ctx, `SELECT state, COALESCE(ARRAY(SELECT jsonb_array_elements_text(scope_json->'repos')), '{}'),
		  success_json, scope_json, budget_json FROM agentrt.mission WHERE mission_id = $1 FOR NO KEY UPDATE`,
			t.mission).Scan(&state, &repos, &success, &scope, &budget); err != nil {
			return err
		}
		switch {
		case !slices.Contains([]string{"proposed", "awaiting_owner", "active", "paused"}, state):
			return block("the mission is " + state)
		case conclusion != "done":
			return block(fmt.Sprintf("the planner's attempt ended %q", conclusion))
		case report == "":
			return block("the planner's attempt has no report")
		}
		var body []byte
		if err = tx.QueryRow(ctx, `SELECT body FROM agentrt.artifact_blob WHERE artifact_id = $1`, report).Scan(&body); err != nil {
			return err
		}
		known := slices.DeleteFunc(slices.Clone(repos), func(r string) bool { _, ok := Repos[r]; return !ok })
		p, err := ParsePlan(body, known)
		if err != nil {
			return block(err.Error())
		}
		maxAttempts, maxHours := budgetInt(budget, "max_attempts"), budgetInt(budget, "max_wall_hours")
		if (p.Mission.MaxAttempts > 0 && p.Mission.MaxAttempts > maxAttempts) || (p.Mission.MaxWallHours > 0 && p.Mission.MaxWallHours > maxHours) {
			return block(fmt.Sprintf("the plan raises the mission's budget (%d attempts, %d hours); a plan may only lower it", maxAttempts, maxHours))
		}
		var questions []string
		for i, q := range p.Questions {
			if ask == nil {
				return fmt.Errorf("%w: no question store is configured", ErrAsk)
			}
			id, err := ask(ctx, Card{Key: planCardKey(taskID, i), Title: q.Question, Body: q.Body, Options: q.Options,
				Recommended: q.Recommended, Meanwhile: q.Meanwhile, OwnerDuty: q.OwnerDuty, TaskID: taskID})
			if errors.Is(err, ErrInvalid) {
				return block(fmt.Sprintf("questions[%d]: %v", i, err))
			}
			if err != nil {
				return fmt.Errorf("%w: %v", ErrAsk, err)
			}
			questions = append(questions, id)
		}
		if err = amendMission(ctx, tx, t.mission, taskID, p.Mission, scope); err != nil {
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
			spec, err := ImplementSpec(pt.Title, pt.Text, verify)
			if err != nil {
				return err
			}
			id, err := PutArtifact(ctx, tx, Seat, "task_spec", "application/json", spec)
			if err != nil {
				return err
			}
			tasks = append(tasks, TaskProposal{Ref: pt.Ref, OffPlanReason: "planned by task " + taskID, Role: "implement",
				Class: "goal", Title: pt.Title, SpecArtifact: id, Effect: "branch", Repo: pt.Repo, BaseCommit: d.base,
				DependsOn: pt.DependsOn})
		}
		if err = checkBatch(tasks); err != nil {
			return block(err.Error())
		}
		seat := Writer{Actor: Seat}
		if err = ensureWriter(ctx, tx, seat, false); err != nil {
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
		if len(questions) > 0 {
			reason += fmt.Sprintf(", questions %s", strings.Join(questions, ", "))
		}
		return moveTask(ctx, tx, "controller", t, "accepted", reason)
	})
	return did, err
}

// amendMission records what a plan says about its mission, when it says
// anything: the criteria replace the mission's, what is out of scope joins
// its scope, a budget the plan gives (never larger, checked before) replaces
// its own.
func amendMission(ctx context.Context, tx pgx.Tx, mission, taskID string, m PlannedMission, scope map[string]any) error {
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

// planCardKey is a plan's i-th card's key in the question store: the same
// card on every pass over the same task.
func planCardKey(taskID string, i int) string {
	sum, _ := canon.Sum(map[string]any{"task": taskID, "card": i, "route": "plans"})
	return strings.TrimPrefix(sum, "sha256:")
}
