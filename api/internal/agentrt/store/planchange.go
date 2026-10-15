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
// the mission) or move_task (Task, Node: the task's step).
type PlanChange struct {
	Op        string          `json:"op"`
	Node      string          `json:"node,omitempty"`
	Parent    string          `json:"parent,omitempty"`
	Kind      string          `json:"kind,omitempty"`
	StepKind  string          `json:"step_kind,omitempty"`
	Title     string          `json:"title,omitempty"`
	MissionID string          `json:"mission_id,omitempty"`
	Rule      json.RawMessage `json:"rule,omitempty"`
	Bind      []Binding       `json:"bind,omitempty"`
	Task      string          `json:"task,omitempty"`
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
		switch c.Op {
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
		default:
			bad = "is one of add_node, retitle, set_done_rule, rebind_step, move_task"
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
// title of a step not started. Anything else is the Owner's.
func standing(ctx context.Context, tx pgx.Tx, c PlanChange) (bool, error) {
	var ok bool
	var err error
	switch {
	case c.Op == "add_node" && c.Kind == "step":
		err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.plan_node p WHERE p.plan_node_id = $1 AND p.node_kind = 'item'
		  AND EXISTS (SELECT 1 FROM agentrt.plan_node s WHERE s.parent_plan_node_id = p.plan_node_id AND s.state = 'active'))`, c.Parent).Scan(&ok)
	case c.Op == "retitle":
		err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.plan_node WHERE plan_node_id = $1 AND node_kind = 'step'
		  AND state = 'planned')`, c.Node).Scan(&ok)
	}
	return ok, err
}

// applyChanges writes the changes under one plan_changed event of the tree,
// caused by the decision, and returns the ids of the nodes it added. Rows
// are locked before the event's stream (review 149 B1). The database checks
// the result: parent and child kinds, a task on a step of its mission, a
// review where its subject is, a rule naming only what its step binds
// (some at commit).
func applyChanges(ctx context.Context, tx pgx.Tx, actor, root string, decided int64, changes []PlanChange) ([]string, error) {
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
		// Numbering (T30): one more than the largest sibling, counting the
		// nodes this change adds to the same parent.
		var next int
		if err = tx.QueryRow(ctx, `SELECT COALESCE(max(ordinal) + 1, 0) FROM agentrt.plan_node WHERE parent_plan_node_id = $1`, c.Parent).Scan(&next); err != nil {
			return nil, err
		}
		for _, a := range adds {
			if a.parent == c.Parent && a.ordinal >= next {
				next = a.ordinal + 1
			}
		}
		sep := "."
		if c.Kind == "step" {
			sep = "-"
		}
		label := parentLabel + sep + strconv.Itoa(next+1)
		id := root + ":" + label
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
		case "retitle", "set_done_rule", "rebind_step":
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
	if _, err := Append(ctx, tx, Event{Stream: PlanStream(root), Type: "plan_changed", Actor: actor, Body: body}); err != nil {
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
			var doc []byte
			if doc, err = canonRule(c.Rule); err == nil {
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
			_, err = tx.Exec(ctx, `UPDATE agentrt.task SET plan_node_id = $2, off_plan = false, off_plan_reason = NULL, version = version + 1,
			  updated_at = clock_timestamp() WHERE task_id = $1`, c.Task, c.Node)
		}
		if err != nil {
			return nil, err
		}
	}
	return ids, nil
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

// Ask creates the Owner's decision card for a plan change and returns its
// question id. key makes a retry find the card it created.
type Ask func(ctx context.Context, card Card) (string, error)

// ErrAsk: the Owner could not be asked (no question store, or it refused
// the card: full, or over its hourly bound); nothing was written.
var ErrAsk = errors.New("store: the Owner cannot be asked now")

// Card is a decision card about a plan change.
type Card struct {
	Key, Title, Body string
}

// Options of a plan change's card; the controller applies the first.
const (
	CardApply  = "Apply the change"
	CardRefuse = "Do not apply it"
)

// ProposePlanChange records a proposal and, within the standing
// authorization, its decision and the change; otherwise it tries the change
// (and rolls it back), asks the Owner and records which question it is. The
// idempotency key is the writer's.
func ProposePlanChange(ctx context.Context, db DB, w Writer, key, root, reason string, changes []PlanChange, ask Ask) (map[string]any, error) {
	if key == "" {
		return nil, invalid("an idempotency key is required")
	}
	if err := checkChanges(root, reason, changes); err != nil {
		return nil, err
	}
	doc, err := changesDoc(changes)
	if err != nil {
		return nil, err
	}
	req := map[string]any{"root": root, "reason": reason, "changes": doc}
	return idempotent(ctx, db, w.Actor, "plan/proposals", key, req, nil, func(tx pgx.Tx) (map[string]any, error) {
		if err := ensureWriter(ctx, tx, w, true); err != nil {
			return nil, err
		}
		if err := lockTree(ctx, tx, root); err != nil {
			return nil, err
		}
		var rootKind string
		if err := tx.QueryRow(ctx, `SELECT node_kind FROM agentrt.plan_node WHERE plan_node_id = $1 AND parent_plan_node_id IS NULL`, root).Scan(&rootKind); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, invalid("%s is not the root of a plan", root)
			}
			return nil, err
		}
		within := true
		for _, c := range changes {
			ok, err := standing(ctx, tx, c)
			if err != nil {
				return nil, err
			}
			within = within && ok
		}
		body := map[string]any{"changes": doc, "reason": reason, "standing": within, "token": w.Token}
		if within {
			proposed, err := Append(ctx, tx, Event{Stream: PlanStream(root), Type: "plan_change_proposed", Actor: w.Actor, Body: body})
			if err != nil {
				return nil, err
			}
			decided, err := Append(ctx, tx, Event{Stream: PlanStream(root), Type: "plan_change_decided", Actor: "controller",
				Body: map[string]any{"proposal_event_id": proposed.EventID, "decision": "applied", "by": "standing authorization"}})
			if err != nil {
				return nil, err
			}
			ids, err := applyChanges(ctx, tx, "controller", root, decided.EventID, changes)
			if err != nil {
				return nil, err
			}
			return map[string]any{"proposal_event_id": proposed.EventID, "decision": "applied", "nodes": nonNil(ids)}, nil
		}
		// Outside it: tried first, with the database's deferred checks run
		// now, and rolled back; a change the database refuses is refused here.
		if err := tryChanges(ctx, tx, root, changes); err != nil {
			return nil, err
		}
		if ask == nil {
			return nil, fmt.Errorf("%w: no question store is configured", ErrAsk)
		}
		q, err := ask(ctx, Card{Key: canonKey(w.Actor, key), Title: "Plan change to " + root + ": " + firstLine(reason),
			Body: cardBody(root, reason, changes)})
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrAsk, err)
		}
		body["question_id"] = q
		proposed, err := Append(ctx, tx, Event{Stream: PlanStream(root), Type: "plan_change_proposed", Actor: w.Actor, Body: body})
		if err != nil {
			return nil, err
		}
		return map[string]any{"proposal_event_id": proposed.EventID, "decision": "", "question_id": q,
			"waiting": "the Owner decides plan changes outside the standing authorization"}, nil
	})
}

// tryChanges applies the changes in a savepoint with every deferred check
// made at once, then rolls them back. Rolling back to the savepoint also
// undoes SET CONSTRAINTS (measured on PostgreSQL 17), so the rest of the
// transaction checks as before.
func tryChanges(ctx context.Context, tx pgx.Tx, root string, changes []PlanChange) error {
	if _, err := tx.Exec(ctx, `SAVEPOINT try_plan_change`); err != nil {
		return err
	}
	_, err := applyChanges(ctx, tx, "controller", root, 0, changes)
	if err == nil {
		_, err = tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`)
	}
	if _, rerr := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT try_plan_change`); rerr != nil && err == nil {
		err = rerr
	}
	return err
}

func canonKey(actor, key string) string {
	sum, _ := canon.Sum(map[string]any{"actor": actor, "key": key, "route": "plan/proposals"})
	return strings.TrimPrefix(sum, "sha256:")
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	if len(line) > 120 {
		line = line[:120]
	}
	return line
}

func cardBody(root, reason string, changes []PlanChange) string {
	var b strings.Builder
	fmt.Fprintf(&b, "A thread proposes to change plan %s.\n\nWhy: %s\n\nChanges:\n", root, reason)
	for _, c := range changes {
		raw, _ := json.Marshal(c)
		fmt.Fprintf(&b, "- %s\n", raw)
	}
	b.WriteString("\nApplying it writes the change under your decision; not applying it leaves the plan as it is.")
	return b.String()
}

// pendingChange is a proposal waiting on the Owner's answer.
type pendingChange struct {
	event    int64
	root     string
	question string
	changes  []PlanChange
}

// DecidePlanChanges is one pass of the plan decisions: each proposal waiting
// on a question whose answer is in gets plan_change_decided. The first
// option (CardApply) applies the change under that decision; any other
// option refuses it; a question withdrawn or expired leaves it unapplied. A
// change that no longer fits when it is applied is decided failed, with
// the database's reason. One that fails to be read does not stop the
// others. It returns how many proposals it decided.
func DecidePlanChanges(ctx context.Context, db DB, question func(context.Context, string) (Question, bool)) (int, error) {
	decided := 0
	var errs []error
	// Every waiting proposal, a page at a time after a cursor, so ones still
	// waiting for an answer never keep later ones from being decided.
	var after int64
	for {
		var page []pendingChange
		err := InTx(ctx, db, func(tx pgx.Tx) error {
			rows, qerr := tx.Query(ctx, `SELECT p.event_id, substr(p.event_stream_id, 6), p.body_json->>'question_id', (p.body_json->'changes')::text
			  FROM agentrt.event p
			  WHERE p.event_type = 'plan_change_proposed' AND p.body_json ? 'question_id' AND p.event_id > $1
			    AND NOT EXISTS (SELECT 1 FROM agentrt.event d WHERE d.event_type = 'plan_change_decided'
			      AND d.event_stream_id = p.event_stream_id AND (d.body_json->>'proposal_event_id')::bigint = p.event_id)
			  ORDER BY p.event_id LIMIT $2`, after, decidePage)
			if qerr != nil {
				return qerr
			}
			var cerr error
			page, cerr = pgx.CollectRows(rows, func(r pgx.CollectableRow) (pendingChange, error) {
				var p pendingChange
				var doc string
				if serr := r.Scan(&p.event, &p.root, &p.question, &doc); serr != nil {
					return p, serr
				}
				return p, json.Unmarshal([]byte(doc), &p.changes)
			})
			return cerr
		})
		if err != nil {
			return decided, errors.Join(append(errs, err)...)
		}
		for _, p := range page {
			did, derr := decideOne(ctx, db, p, question)
			if derr != nil {
				errs = append(errs, fmt.Errorf("plan change %d: %w", p.event, derr))
			}
			if did {
				decided++
			}
		}
		if len(page) < decidePage {
			return decided, errors.Join(errs...)
		}
		after = page[len(page)-1].event
	}
}

// decidePage is how many waiting proposals a pass reads at a time.
const decidePage = 100

func decideOne(ctx context.Context, db DB, p pendingChange, question func(context.Context, string) (Question, bool)) (bool, error) {
	q, ok := question(ctx, p.question)
	switch {
	case !ok:
		return false, nil
	case q.Status == QuestionAnswered && q.Option == CardApply:
		return decide(ctx, db, p, "applied", "")
	case q.Status == QuestionAnswered:
		return decide(ctx, db, p, "refused", "")
	case q.Status == "withdrawn" || q.Status == "expired":
		return decide(ctx, db, p, q.Status, "")
	}
	return false, nil
}

// decide records a decision under the tree's lock, once, and applies the
// change when it is applied; a change the database refuses then is decided
// failed instead, with its reason.
func decide(ctx context.Context, db DB, p pendingChange, decision, reason string) (bool, error) {
	did := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		if err := lockTree(ctx, tx, p.root); err != nil {
			return err
		}
		var done bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE event_type = 'plan_change_decided'
		  AND event_stream_id = $1 AND (body_json->>'proposal_event_id')::bigint = $2)`, PlanStream(p.root), p.event).Scan(&done); err != nil || done {
			return err
		}
		body := map[string]any{"proposal_event_id": p.event, "decision": decision, "by": "owner", "question_id": p.question}
		if reason != "" {
			body["error"] = reason
		}
		ev, err := Append(ctx, tx, Event{Stream: PlanStream(p.root), Type: "plan_change_decided", Actor: "controller", Body: body})
		if err != nil {
			return err
		}
		if decision == "applied" {
			if _, err = applyChanges(ctx, tx, "controller", p.root, ev.EventID, p.changes); err != nil {
				return err
			}
		}
		did = true
		return nil
	})
	if err != nil && decision == "applied" && refusedByRules(err) {
		return decide(ctx, db, p, "failed", err.Error())
	}
	return did, err
}

// refusedByRules: the change broke a rule of its own or of the database,
// not a passing failure.
func refusedByRules(err error) bool {
	var pg *pgconn.PgError
	return errors.Is(err, ErrInvalid) || (errors.As(err, &pg) && (pg.Code == "23502" || pg.Code == "23503" || pg.Code == "23505" ||
		pg.Code == "23514" || pg.Code == "P0001"))
}
