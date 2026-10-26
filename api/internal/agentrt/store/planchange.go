package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
)

// Plan changes (v5 §5.2: POST /plan/proposals). A thread or seat proposes
// changes to one plan tree: plan_change_proposed. Within the standing
// authorization (a step added to an item in progress, the title of a step
// not started) the change is decided and applied at once; otherwise it is
// tried and rolled back, so a change the database would refuse is refused
// now with its reason (T32 through the interface), and the Owner gets a
// decision card; the controller applies it once he answers. A change lands
// as one plan_changed event that names the decision, with every row it
// writes.

// Binding is an approval or a question a done rule may name (v5 §5.1.2).
type Binding struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

// PlanChange is one change. Op is add_node (Parent, Kind, Title, and for a
// step StepKind and MissionID), retitle (Node, Title), set_done_rule (Node,
// Rule, Bind), rebind_step (Node, MissionID: the step and its tasks move to
// the mission), move_task (Task, Node: the task's step), drop_step (Node: the
// step leaves the count, keeping its number) or set_marks (Node, and
// OwnerStep, EffectStep or both: the step's 「（你）」 and ★ marks).
type PlanChange struct {
	Op         string          `json:"op"`
	Node       string          `json:"node,omitempty"`
	Parent     string          `json:"parent,omitempty"`
	Kind       string          `json:"kind,omitempty"`
	StepKind   string          `json:"step_kind,omitempty"`
	Title      string          `json:"title,omitempty"`
	MissionID  string          `json:"mission_id,omitempty"`
	Rule       json.RawMessage `json:"rule,omitempty"`
	Bind       []Binding       `json:"bind,omitempty"`
	Task       string          `json:"task,omitempty"`
	OwnerStep  *bool           `json:"owner_step,omitempty"`
	EffectStep *bool           `json:"effect_step,omitempty"`
	// Artifact is the request an import proposes (op import, only through
	// POST /plan/imports; planimportcard.go).
	Artifact        string `json:"artifact,omitempty"`
	ExpectedVersion *int   `json:"expected_version,omitempty"`
}

// MaxChanges is the most changes one proposal holds.
const MaxChanges = 20

var stepKinds = []string{"agent", "review", "owner", "approval", "release", "ci", "gate", "manual"}

func checkChanges(root, reason string, changes []PlanChange) error {
	switch {
	case root == "":
		return invalid("a plan change names its root")
	case reason == "":
		return invalid("a plan change says why")
	case len(changes) == 0 || len(changes) > MaxChanges:
		return invalid("a proposal holds 1 to %d changes", MaxChanges)
	}
	for i, c := range changes {
		var bad string
		if c.Op != "adopt_plan_view" && (c.Artifact != "" || c.ExpectedVersion != nil) {
			return invalid("artifact and expected_version belong only to adopt_plan_view")
		}
		switch c.Op {
		case "adopt_plan_view":
			if c.Node != "" || c.Parent != "" || c.Kind != "" || c.StepKind != "" || c.Title != "" || c.MissionID != "" || len(c.Rule) > 0 || len(c.Bind) > 0 || c.Task != "" || c.OwnerStep != nil || c.EffectStep != nil {
				return invalid("adopt_plan_view takes only artifact and expected_version")
			}
			if len(changes) != 1 || c.Artifact == "" || c.ExpectedVersion == nil || *c.ExpectedVersion < 0 {
				bad = "requires one artifact adoption and expected_version >= 0"
			}
		case "add_node":
			switch {
			case c.Parent == "" || c.Title == "":
				bad = "names its parent and title"
			case c.Kind != "item" && c.Kind != "step":
				bad = "adds an item or a step"
			case c.Kind == "step" && !slices.Contains(stepKinds, c.StepKind):
				bad = fmt.Sprintf("gives a step one of the step kinds %v", stepKinds)
			case c.Kind == "item" && (c.StepKind != "" || c.MissionID != ""):
				bad = "gives an item no step kind or mission"
			}
		case "retitle":
			if c.Node == "" || c.Title == "" {
				bad = "names the node and its new title"
			}
		case "set_done_rule":
			if c.Node == "" || len(c.Rule) == 0 {
				bad = "names the node and the rule"
			} else if _, err := plan.Parse(c.Rule); err != nil {
				bad = "has a rule that does not parse: " + err.Error()
			}
			for _, b := range c.Bind {
				if (b.Kind != "approval" && b.Kind != "question") || b.Ref == "" {
					bad = "binds approvals and questions by their ref"
				}
			}
		case "rebind_step":
			if c.Node == "" || c.MissionID == "" {
				bad = "names the step and its new mission"
			}
		case "move_task":
			if c.Task == "" || c.Node == "" {
				bad = "names the task and its new step"
			}
		case "drop_step":
			if c.Node == "" {
				bad = "names the step"
			}
		case "set_marks":
			if c.Node == "" || (c.OwnerStep == nil && c.EffectStep == nil) {
				bad = "names the step and owner_step, effect_step or both"
			}
		default:
			bad = "is one of add_node, retitle, set_done_rule, rebind_step, move_task, drop_step, set_marks"
		}
		if bad != "" {
			return invalid("change %d (%s) %s", i, c.Op, bad)
		}
	}
	return nil
}

// lockTree serializes the changes of one plan tree: numbering, the standing
// check and the application read and write it under this lock.
func lockTree(ctx context.Context, tx pgx.Tx, root string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(742110, hashtext($1))`, root)
	return err
}

// standing reports whether a change is within the standing authorization:
// a step added to an item in progress (one of its steps is active), or the
// title of a step not started; and, while the plan-standing rule in force
// grants done_rules, the first done rule of a step not done that names no
// manual atom (CUT-plan-import-2026-10-19.md §7). Anything else is the
// Owner's.
func standing(ctx context.Context, tx pgx.Tx, c PlanChange, rule *StoredRule) (bool, error) {
	var ok bool
	var err error
	switch {
	case c.Op == "set_done_rule":
		return standingRule(ctx, tx, c, rule)
	case c.Op == "add_node" && c.Kind == "step":
		err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.plan_node p WHERE p.plan_node_id = $1 AND p.node_kind = 'item'
		  AND EXISTS (SELECT 1 FROM agentrt.plan_node s WHERE s.parent_plan_node_id = p.plan_node_id AND s.state = 'active'))`, c.Parent).Scan(&ok)
	case c.Op == "retitle":
		err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.plan_node WHERE plan_node_id = $1 AND node_kind = 'step'
		  AND state = 'planned')`, c.Node).Scan(&ok)
	}
	return ok, err
}

// standingRule is the plan-standing grant for a done rule (reviews 313 A2,
// 314 A2): only while the rule in force grants done_rules; only the first
// rule of a step not done; binding nothing new (a new binding is the
// Owner's); naming only facts the runtime itself keeps, the step's own
// tasks and the Owner's sign-off on it, never an approval or a question (an
// outside record the runtime may not have read yet, so that it does not hold
// cannot be shown), a manual atom, a CI run (a fact of no step) or a gate;
// and not already holding, so no rule makes a step done at once.
func standingRule(ctx context.Context, tx pgx.Tx, c PlanChange, rule *StoredRule) (bool, error) {
	if rule == nil || !rule.Plan.DoneRules || len(c.Bind) > 0 {
		return false, nil
	}
	parsed, err := plan.Parse(c.Rule)
	if err != nil || parsed.Empty() {
		return false, nil
	}
	for _, atom := range []string{plan.AtomManual, plan.AtomCI, plan.AtomGates, plan.AtomApproval, plan.AtomQuestion} {
		if parsed.Names(atom) {
			return false, nil
		}
	}
	var first bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.plan_node WHERE plan_node_id = $1 AND node_kind = 'step'
	  AND state IN ('planned','active') AND done_rule_json = '{}'::jsonb)`, c.Node).Scan(&first); err != nil || !first {
		return false, err
	}
	f := &stepFacts{ctx: ctx, tx: tx, step: c.Node, deployed: func(context.Context, string) (string, map[string]any, bool) { return "", nil, false }}
	res := parsed.Eval(f)
	if f.err != nil {
		return false, f.err
	}
	return res.Value != plan.True, nil
}

// lockChanges takes, before any event of the tree's stream (review 180 A3),
// every row the changes write or rest on: steps before tasks, each in id
// order. The nodes they change, and the steps a task moves between, FOR
// UPDATE; the parent a node is added under, and that parent's steps (which
// prove an item in progress), FOR SHARE; then the tasks they move. It then
// checks that every node is in the tree (review 180 A2). The standing check
// and the changes read what these locks hold.
func lockChanges(ctx context.Context, tx pgx.Tx, root string, changes []PlanChange) error {
	modes := map[string]string{}
	want := func(id, mode string) {
		if id != "" && modes[id] != "UPDATE" {
			modes[id] = mode
		}
	}
	var moved, rebound []string
	from := map[string]string{}
	for _, c := range changes {
		switch c.Op {
		case "add_node":
			want(c.Parent, "SHARE")
			rows, err := tx.Query(ctx, `SELECT plan_node_id FROM agentrt.plan_node WHERE parent_plan_node_id = $1 AND node_kind = 'step'`, c.Parent)
			if err != nil {
				return err
			}
			steps, err := pgx.CollectRows(rows, pgx.RowTo[string])
			if err != nil {
				return err
			}
			for _, id := range steps {
				want(id, "SHARE")
			}
		case "retitle", "set_done_rule", "drop_step", "set_marks":
			want(c.Node, "UPDATE")
		case "rebind_step":
			want(c.Node, "UPDATE")
			rebound = append(rebound, c.Node)
		case "move_task":
			want(c.Node, "UPDATE")
			moved = append(moved, c.Task)
			var step *string
			err := tx.QueryRow(ctx, `SELECT plan_node_id FROM agentrt.task WHERE task_id = $1`, c.Task).Scan(&step)
			if errors.Is(err, pgx.ErrNoRows) {
				return invalid("there is no task %s", c.Task)
			}
			if err != nil {
				return err
			}
			if step != nil {
				from[c.Task] = *step
				want(*step, "UPDATE")
			}
		}
	}
	hook("change read", "")
	ids := make([]string, 0, len(modes))
	for id := range modes {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		var one int
		err := tx.QueryRow(ctx, `SELECT 1 FROM agentrt.plan_node WHERE plan_node_id = $1 FOR `+modes[id], id).Scan(&one)
		if errors.Is(err, pgx.ErrNoRows) {
			return invalid("there is no node %s", id)
		}
		if err != nil {
			return err
		}
		in, err := rootOf(ctx, tx, id)
		if err != nil {
			return err
		}
		if in != root {
			return invalid("node %s is in plan %s, not %s", id, in, root)
		}
	}
	// The tasks NOWAIT: Dispatch locks a task before the tasks it depends
	// on, Conclude a subject before its check, so no one order of task locks
	// fits them all (review 182 A3); a change that finds one held fails at
	// once with lock_not_available and is tried again whole.
	slices.Sort(moved)
	if _, err := tx.Exec(ctx, `SELECT 1 FROM agentrt.task WHERE task_id = ANY($1) OR plan_node_id = ANY($2) ORDER BY task_id
	  FOR NO KEY UPDATE NOWAIT`, moved, rebound); err != nil {
		return err
	}
	// And their subjects, which the task trigger locks FOR SHARE when a
	// verification or review moves (review 183 A3): taken here, NOWAIT, so
	// the trigger never waits for one while this holds the moved task.
	if _, err := tx.Exec(ctx, `SELECT 1 FROM agentrt.task WHERE task_id IN (SELECT subject_task_id FROM agentrt.task
	  WHERE (task_id = ANY($1) OR plan_node_id = ANY($2)) AND subject_task_id IS NOT NULL) ORDER BY task_id FOR SHARE NOWAIT`,
		moved, rebound); err != nil {
		return err
	}
	// Each moved task is still where it was read (another tree may have
	// moved it in between; review 182 A2); if not, the step locks taken are
	// the wrong ones: try again whole.
	for _, task := range moved {
		var step *string
		if err := tx.QueryRow(ctx, `SELECT plan_node_id FROM agentrt.task WHERE task_id = $1`, task).Scan(&step); err != nil {
			return err
		}
		now := ""
		if step != nil {
			now = *step
		}
		if now != from[task] {
			return fmt.Errorf("%w: task %s moved from %q to %q while it was locked", errMoved, task, from[task], now)
		}
	}
	return nil
}

// errMoved: a task a change moves was moved by someone else after it was
// read; the change is tried again whole.
var errMoved = errors.New("store: a task moved while the change locked it")

// retryable: a change that met a held row or a moved task; nothing was
// written and it may simply be tried again.
func retryable(err error) bool {
	return lockNotAvailable(err) || errors.Is(err, errMoved) || deadlocked(err)
}

// deadlocked: PostgreSQL broke a lock cycle by ending this transaction;
// nothing it wrote stays, and it may be tried again.
func deadlocked(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "40P01"
}

// rootOf is the root of a node's tree, along its parents.
func rootOf(ctx context.Context, tx pgx.Tx, id string) (string, error) {
	var root string
	err := tx.QueryRow(ctx, `WITH RECURSIVE up AS (
	    SELECT plan_node_id, parent_plan_node_id FROM agentrt.plan_node WHERE plan_node_id = $1
	    UNION ALL
	    SELECT p.plan_node_id, p.parent_plan_node_id FROM agentrt.plan_node p JOIN up ON p.plan_node_id = up.parent_plan_node_id)
	  SELECT plan_node_id FROM up WHERE parent_plan_node_id IS NULL`, id).Scan(&root)
	return root, err
}

// applyChanges writes the changes under one plan_changed event of the tree,
// caused by the decision, and returns the ids of the nodes it added. Rows
// are locked before the event's stream (review 149 B1). The database checks
// the result: parent and child kinds, a task on a step of its mission, a
// review where its subject is, a rule naming only what its step binds
// (some at commit).
func applyChanges(ctx context.Context, tx pgx.Tx, actor, root string, decided int64, changes []PlanChange) ([]string, error) {
	if len(changes) == 1 && changes[0].Op == "adopt_plan_view" {
		return nil, adoptPlanView(ctx, tx, actor, root, decided, changes[0])
	}
	for _, c := range changes {
		node := c.Node
		if c.Op == "add_node" {
			node = c.Parent
		}
		mission := c.MissionID
		if c.Op == "move_task" {
			if err := tx.QueryRow(ctx, `SELECT mission_id FROM agentrt.task WHERE task_id=$1`, c.Task).Scan(&mission); err != nil {
				return nil, err
			}
		}
		if node != "" && mission != "" {
			if err := checkViewBinding(ctx, tx, node, mission); err != nil {
				return nil, err
			}
		}
	}
	for _, c := range changes {
		if err := gateGuard(ctx, tx, c); err != nil { // gatesplan.go
			return nil, err
		}
	}
	type newNode struct {
		id, parent, kind, label, title, stepKind, mission string
		ordinal                                           int
	}
	var adds []newNode
	var ids []string
	for _, c := range changes {
		if c.Op != "add_node" {
			continue
		}
		var parentKind, parentLabel string
		err := tx.QueryRow(ctx, `SELECT node_kind, label FROM agentrt.plan_node WHERE plan_node_id = $1 FOR SHARE`, c.Parent).Scan(&parentKind, &parentLabel)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, invalid("there is no node %s", c.Parent)
		}
		if err != nil {
			return nil, err
		}
		// Numbering (T30): the position is one past the largest sibling's;
		// the number in the id and label is one past the largest number the
		// siblings' labels carry after the parent's label, counting the nodes
		// this proposal adds, skipping any id taken (review 180 B1).
		var next int
		if err = tx.QueryRow(ctx, `SELECT COALESCE(max(ordinal) + 1, 0) FROM agentrt.plan_node WHERE parent_plan_node_id = $1`, c.Parent).Scan(&next); err != nil {
			return nil, err
		}
		sep := "."
		if c.Kind == "step" {
			sep = "-"
		}
		prefix := parentLabel + sep
		rows, err := tx.Query(ctx, `SELECT label FROM agentrt.plan_node WHERE parent_plan_node_id = $1`, c.Parent)
		if err != nil {
			return nil, err
		}
		labels, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return nil, err
		}
		number := 0
		for _, a := range adds {
			if a.parent == c.Parent {
				labels = append(labels, a.label)
				if a.ordinal >= next {
					next = a.ordinal + 1
				}
			}
		}
		for _, l := range labels {
			if n, aerr := strconv.Atoi(strings.TrimPrefix(l, prefix)); aerr == nil && strings.HasPrefix(l, prefix) && n > number {
				number = n
			}
		}
		var label, id string
		for {
			number++
			label = prefix + strconv.Itoa(number)
			id = root + ":" + label
			var taken bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.plan_node WHERE plan_node_id = $1)`, id).Scan(&taken); err != nil {
				return nil, err
			}
			if !taken && !slices.Contains(ids, id) {
				break
			}
		}
		adds = append(adds, newNode{id, c.Parent, c.Kind, label, c.Title, c.StepKind, c.MissionID, next})
		ids = append(ids, id)
	}
	var bindings []any
	for _, c := range changes {
		for _, b := range c.Bind {
			bindings = append(bindings, map[string]any{"plan_node_id": c.Node, "kind": b.Kind, "ref": b.Ref})
		}
	}
	// The nodes and tasks the other changes write, locked for update before
	// the event (the step migration's lock of v5 §5.1.1 「步骤锁」).
	for _, c := range changes {
		var q, ref string
		switch c.Op {
		case "retitle", "set_done_rule", "rebind_step", "drop_step", "set_marks":
			q, ref = `SELECT 1 FROM agentrt.plan_node WHERE plan_node_id = $1 FOR UPDATE`, c.Node
		case "move_task":
			q, ref = `SELECT 1 FROM agentrt.task WHERE task_id = $1 FOR NO KEY UPDATE`, c.Task
		default:
			continue
		}
		var one int
		if err := tx.QueryRow(ctx, q, ref).Scan(&one); errors.Is(err, pgx.ErrNoRows) {
			return nil, invalid("there is no %s", ref)
		} else if err != nil {
			return nil, err
		}
	}
	summary := make([]string, 0, len(changes))
	for _, c := range changes {
		summary = append(summary, c.Op)
	}
	body := map[string]any{"change": strings.Join(summary, ", "), "decided_event_id": decided}
	if len(ids) > 0 {
		body["nodes"] = ids
	}
	if len(bindings) > 0 {
		body["bindings"] = bindings
	}
	changed, err := Append(ctx, tx, Event{Stream: PlanStream(root), Type: "plan_changed", Actor: actor, Body: body})
	if err != nil {
		return nil, err
	}
	for _, a := range adds {
		var state any
		if a.kind == "step" {
			state = "planned"
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.plan_node (plan_node_id, parent_plan_node_id, node_kind, ordinal, label, title,
		  mission_id, step_kind, state, created_by_actor_id) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			a.id, a.parent, a.kind, a.ordinal, a.label, a.title, nullable(a.mission), nullable(a.stepKind), state, actor); err != nil {
			return nil, err
		}
	}
	for _, c := range changes {
		var err error
		switch c.Op {
		case "retitle":
			_, err = tx.Exec(ctx, `UPDATE agentrt.plan_node SET title = $2, version = version + 1 WHERE plan_node_id = $1`, c.Node, c.Title)
		case "set_done_rule":
			// An architecture row is ticked by gate tests only, as it is
			// imported (planimport.ParseArch); a fact that stays true once
			// recorded would tick it whatever runs (review 427 A2).
			var kind string
			if err = tx.QueryRow(ctx, `SELECT node_kind FROM agentrt.plan_node WHERE plan_node_id = $1`, c.Node).Scan(&kind); err != nil {
				return nil, err
			}
			if kind == "arch_row" {
				if rule, perr := plan.Parse(c.Rule); perr != nil || !(rule.Empty() || rule.OnlyStanding()) {
					return nil, invalid("%s is an architecture row: its done rule is gate tests only (gates_passed)", c.Node)
				}
			}
			var doc []byte
			if doc, err = canonRule(c.Rule); err == nil {
				parsed, _ := plan.Parse(doc)
				if err = checkBoundRule(ctx, tx, c.Node, parsed); err != nil {
					return nil, err
				}
				_, err = tx.Exec(ctx, `UPDATE agentrt.plan_node SET done_rule_json = $2::jsonb, version = version + 1 WHERE plan_node_id = $1`, c.Node, string(doc))
			}
			for _, b := range c.Bind {
				if err == nil {
					_, err = tx.Exec(ctx, `INSERT INTO agentrt.plan_node_binding (plan_node_id, bound_kind, bound_ref, event_id)
					  VALUES ($1, $2, $3, current_setting('agentrt.event_id')::bigint) ON CONFLICT DO NOTHING`, c.Node, b.Kind, b.Ref)
				}
			}
		case "rebind_step":
			if _, err = tx.Exec(ctx, `UPDATE agentrt.plan_node SET mission_id = $2, version = version + 1 WHERE plan_node_id = $1`,
				c.Node, c.MissionID); err == nil {
				_, err = tx.Exec(ctx, `UPDATE agentrt.task SET mission_id = $2, version = version + 1, updated_at = clock_timestamp()
				  WHERE plan_node_id = $1`, c.Node, c.MissionID)
			}
		case "move_task":
			// Not onto a dropped step (review 313 A3); the step is locked
			// above.
			var state *string
			if err = tx.QueryRow(ctx, `SELECT state FROM agentrt.plan_node WHERE plan_node_id = $1`, c.Node).Scan(&state); err == nil &&
				state != nil && *state == "dropped" {
				err = invalid("step %s is dropped; no task moves onto it", c.Node)
			}
			if err == nil {
				_, err = tx.Exec(ctx, `UPDATE agentrt.task SET plan_node_id = $2, off_plan = false, off_plan_reason = NULL, version = version + 1,
				  updated_at = clock_timestamp() WHERE task_id = $1`, c.Task, c.Node)
			}
		case "set_marks":
			err = setMarks(ctx, tx, c)
		}
		if err != nil {
			return nil, err
		}
	}
	// Drops last: each writes a state change event of its own, which every
	// row written after it would be checked against.
	for _, c := range changes {
		if c.Op == "drop_step" {
			if err := dropStep(ctx, tx, actor, root, c.Node, changed.EventID); err != nil {
				return nil, err
			}
		}
	}
	return ids, nil
}

// setMarks sets a step's Owner and ★ marks under the plan_changed event.
func setMarks(ctx context.Context, tx pgx.Tx, c PlanChange) error {
	tag, err := tx.Exec(ctx, `UPDATE agentrt.plan_node SET owner_step = COALESCE($2, owner_step), effect_step = COALESCE($3, effect_step),
	  version = version + 1 WHERE plan_node_id = $1 AND node_kind = 'step'`, c.Node, c.OwnerStep, c.EffectStep)
	if err == nil && tag.RowsAffected() == 0 {
		err = invalid("%s is not a step: only a step has marks", c.Node)
	}
	return err
}

// dropStep moves a step to dropped with its own plan_node_state_changed
// event, caused by the plan change: it keeps its number and leaves the
// count. A step with work still open on it is not dropped.
func dropStep(ctx context.Context, tx pgx.Tx, actor, root, step string, cause int64) error {
	var kind string
	var state, mission *string
	var open int
	err := tx.QueryRow(ctx, `SELECT n.node_kind, n.state, n.mission_id, (SELECT count(*) FROM agentrt.task t WHERE t.plan_node_id = n.plan_node_id
	    AND t.state NOT IN ('accepted','rejected','cancelled'))
	  FROM agentrt.plan_node n WHERE n.plan_node_id = $1`, step).Scan(&kind, &state, &mission, &open)
	switch {
	case err != nil:
		return err
	case kind != "step":
		return invalid("%s is a %s: only a step is dropped", step, kind)
	case *state == "dropped":
		return invalid("step %s is dropped already", step)
	case open > 0:
		return invalid("step %s has %d unfinished tasks; cancel them before it is dropped", step, open)
	}
	m := ""
	if mission != nil {
		m = *mission
	}
	ev, err := Append(ctx, tx, Event{Stream: PlanStream(root), Type: "plan_node_state_changed", Actor: actor, PlanNode: step, MissionID: m,
		Cause: cause, Body: map[string]any{"from": *state, "to": "dropped", "cause": "plan_changed"}})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE agentrt.plan_node SET state = 'dropped', state_event_id = $2, version = version + 1 WHERE plan_node_id = $1`,
		step, ev.EventID)
	return err
}

func canonRule(raw json.RawMessage) ([]byte, error) {
	v, err := canon.Parse(raw)
	if err != nil {
		return nil, invalid("the rule is not canonical JSON: %v", err)
	}
	return canon.Encode(v)
}

func changesDoc(changes []PlanChange) (any, error) {
	raw, err := json.Marshal(changes)
	if err != nil {
		return nil, err
	}
	return canon.Parse(raw)
}
