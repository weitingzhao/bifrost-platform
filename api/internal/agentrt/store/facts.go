package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
)

// stepRoot is the round a step belongs to, which names its event stream.
func stepRoot(ctx context.Context, tx pgx.Tx, stepID string) (string, error) {
	var root string
	err := tx.QueryRow(ctx, `SELECT r.plan_node_id FROM agentrt.plan_node s
	  JOIN agentrt.plan_node i ON i.plan_node_id = s.parent_plan_node_id
	  JOIN agentrt.plan_node r ON r.plan_node_id = i.parent_plan_node_id
	  WHERE s.plan_node_id = $1 AND s.node_kind = 'step'`, stepID).Scan(&root)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("%w: %s is not a step", ErrState, stepID)
	}
	return root, err
}

// SignOff records the Owner's sign-off on a step (owner_signoff, carrying
// the step), the fact an owner_signoff atom waits for.
func SignOff(ctx context.Context, tx pgx.Tx, actor, stepID, ref string) error {
	_, err := signOff(ctx, tx, actor, stepID, ref)
	return err
}

func signOff(ctx context.Context, tx pgx.Tx, actor, stepID, ref string, evidence ...string) (Appended, error) {
	root, err := stepRoot(ctx, tx, stepID)
	if err != nil {
		return Appended{}, err
	}
	body, err := acceptanceSignoffBody(ctx, tx, actor, stepID, ref, evidence)
	if err != nil {
		return Appended{}, err
	}
	return Append(ctx, tx, Event{Stream: PlanStream(root), Type: "owner_signoff", Actor: actor, PlanNode: stepID, Body: body})
}

// RecordManual records a manual atom of a step as done: a note on the step
// naming the atom and the evidence for it, which must be stored here.
func RecordManual(ctx context.Context, tx pgx.Tx, actor, stepID, manual, text string, evidence []string) error {
	if len(evidence) == 0 {
		return fmt.Errorf("%w: a manual record needs evidence", ErrState)
	}
	if err := artifact.HaveArtifacts(ctx, tx, evidence); err != nil {
		return err
	}
	root, err := stepRoot(ctx, tx, stepID)
	if err != nil {
		return err
	}
	_, err = Append(ctx, tx, Event{Stream: PlanStream(root), Type: "note", Actor: actor, PlanNode: stepID,
		Body: map[string]any{"text": text, "manual": manual, "evidence": evidence}})
	return err
}

// RuledNode is a plan node with a done rule.
type RuledNode struct {
	ID, Kind, State string
	Rule            plan.Rule
}

// RuledNodes lists the steps that are neither dropped nor stopped and the
// architecture rows, each with its parsed done rule.
func RuledNodes(ctx context.Context, db DB) ([]RuledNode, error) {
	var out []RuledNode
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT plan_node_id, node_kind, COALESCE(state, ''), done_rule_json::text FROM agentrt.plan_node
		  WHERE done_rule_json <> '{}'::jsonb AND (node_kind = 'arch_row' OR state NOT IN ('dropped','stopped'))
		  ORDER BY plan_node_id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var n RuledNode
			var doc string
			if err = rows.Scan(&n.ID, &n.Kind, &n.State, &doc); err != nil {
				return err
			}
			if n.Rule, err = plan.Parse([]byte(doc)); err != nil {
				return fmt.Errorf("store: node %s: %w", n.ID, err)
			}
			out = append(out, n)
		}
		return rows.Err()
	})
	return out, err
}

// stepFacts answers a step's atoms from its own records only (v5 §5.1.2
// 「只能引用绑在这一步上的对象」). A database error is kept and ends the
// evaluation without a state change.
type stepFacts struct {
	planFacts *planFactsSnapshot
	ctx       context.Context
	tx        pgx.Tx
	step      string
	deployed  seenfacts.Deployed
	err       error
}

func (f *stepFacts) exists(q string, args ...any) bool {
	if f.err != nil {
		return false
	}
	var ok bool
	f.err = f.tx.QueryRow(f.ctx, q, args...).Scan(&ok)
	return ok && f.err == nil
}

// hasBody asks the ledger for an event of the type, of the plan node when
// one is named, whose body contains doc (eventlog.HasBody); like exists, a
// failed read is kept and answers false.
func (f *stepFacts) hasBody(typ, planNode string, doc map[string]any) bool {
	if f.err != nil {
		return false
	}
	var ok bool
	ok, f.err = eventlog.HasBody(f.ctx, f.tx, typ, planNode, doc)
	return ok && f.err == nil
}

func (f *stepFacts) TaskAccepted(taskID string) bool {
	// No task is accepted any more and the tasks' table is not read (store
	// cut 4, 2026-11-14). A step that became done by this atom stays done:
	// a done step without a standing condition is not evaluated again.
	return false
}

func (f *stepFacts) ApprovalExecuted(number int64) bool {
	return f.exists(`SELECT EXISTS (SELECT 1 FROM agentrt.plan_node_binding WHERE plan_node_id = $1 AND bound_kind = 'approval' AND bound_ref = $2)
	  AND EXISTS (SELECT 1 FROM agentrt.event WHERE event_type = 'approval_seen'
	              AND body_json @> jsonb_build_object('approval', $3::bigint, 'status', $4::text))`,
		f.step, strconv.FormatInt(number, 10), number, seenfacts.ApprovalExecuted)
}

func (f *stepFacts) QuestionAnswered(question, option string) bool {
	return f.exists(`SELECT EXISTS (SELECT 1 FROM agentrt.plan_node_binding WHERE plan_node_id = $1 AND bound_kind = 'question' AND bound_ref = $2)
	  AND EXISTS (SELECT 1 FROM agentrt.event WHERE event_type = 'question_seen'
	              AND body_json @> jsonb_build_object('question', $2::text, 'status', $3::text, 'option', $4::text))`,
		f.step, question, seenfacts.QuestionAnswered, option)
}

func (f *stepFacts) CISucceeded(repo, commit string) bool {
	return f.hasBody("ci_seen", "", map[string]any{"repo": repo, "commit": commit, "status": seenfacts.CISucceeded})
}

func (f *stepFacts) OwnerSignoff(ref string) bool {
	return f.hasBody("owner_signoff", f.step, map[string]any{"ref": ref})
}

func (f *stepFacts) ManualRecorded(note string) bool {
	return f.exists(`SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE event_type = 'note' AND plan_node_id = $1
	  AND body_json @> jsonb_build_object('manual', $2::text) AND body_json ? 'evidence')`, f.step, note)
}

// GatePassed is always unknown: gates ran as tasks of the dispatch lane and
// left their verdicts as verifications, and nothing runs one any more (the
// coordinator, 2026-11-14; the last batch ran on 2026-10-22). A rule that
// names gates still parses; it neither holds nor fails.
func (f *stepFacts) GatePassed(env, test, specSHA256 string) plan.Tri {
	return plan.Unknown
}

// EvaluateStep evaluates a step's done rule and moves the step when the rule
// says so: planned or active to done when it holds; done back to active
// (standing_lapsed) when a standing condition no longer holds. An unknown
// result moves nothing. It returns the state it moved to, or "".
func EvaluateStep(ctx context.Context, db DB, stepID string, deployed seenfacts.Deployed) (string, error) {
	var moved string
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		var err error
		moved, err = evaluateStep(ctx, tx, stepID, deployed)
		return err
	})
	return moved, err
}

// evaluateStep is EvaluateStep inside tx; a plan import runs it on the steps
// it brings in done (planimport.go).
func evaluateStep(ctx context.Context, tx pgx.Tx, stepID string, deployed seenfacts.Deployed) (string, error) {
	var state, doc string
	if err := tx.QueryRow(ctx, `SELECT state, done_rule_json::text FROM agentrt.plan_node
	  WHERE plan_node_id = $1 AND node_kind = 'step' FOR UPDATE`, stepID).Scan(&state, &doc); err != nil {
		return "", err
	}
	rule, err := plan.Parse([]byte(doc))
	if err != nil {
		return "", fmt.Errorf("store: step %s: %w", stepID, err)
	}
	// A done step moves again only when a standing condition lapses, so a
	// done step without one is not read (its rule may fold landing streams).
	if rule.Empty() || state == "dropped" || state == "stopped" || state == "done" && !rule.Standing() {
		return "", nil
	}
	f := &stepFacts{ctx: ctx, tx: tx, step: stepID, deployed: deployed}
	res := rule.Eval(f)
	if f.err != nil {
		return "", f.err
	}
	to, cause := "", ""
	switch {
	case res.Value == plan.True && (state == "planned" || state == "active"):
		to, cause = "done", "done_rule"
		if res.Manual {
			cause = "done_rule_manual"
		}
	case (res.Value == plan.False || res.Value == plan.Unknown && len(rule.BoundRefs()) > 0) && state == "done" && rule.Standing():
		to, cause = "active", "standing_lapsed"
	default:
		return "", nil
	}
	root, err := stepRoot(ctx, tx, stepID)
	if err != nil {
		return "", err
	}
	if _, err := SetStepState(ctx, tx, "controller", root, stepID, to, cause); err != nil {
		return "", err
	}
	return to, nil
}
