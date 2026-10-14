package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
)

// External facts and step evaluation (design v5 §5.1.2 「done_rule」, §5.2
// 控制器「完成规则」「外部事实」). Approvals, questions, CI runs and deployments
// live outside the runtime; the controller reads them and records each change
// as an event, which is the immutable copy rules are evaluated against
// (answered questions are trimmed from their store later).

// The words the sources report for the states rules wait on.
const (
	ApprovalExecuted = "executed"
	QuestionAnswered = "answered"
	CISucceeded      = "Succeeded"
)

// ExternalStream is the event stream of external facts.
const ExternalStream = "external"

// seen records an outside record's state when it differs from the last one
// recorded under the same key, and reports whether it recorded. Calls for one
// key run one at a time, so two controllers never record one change twice.
func seen(ctx context.Context, db DB, typ string, key, body map[string]any) (bool, error) {
	keyDoc, err := canon.Encode(key)
	if err != nil {
		return false, err
	}
	want, err := canon.Encode(body)
	if err != nil {
		return false, err
	}
	var recorded bool
	err = InTx(ctx, db, func(tx pgx.Tx) error {
		var e error
		recorded, e = recordIfChanged(ctx, tx, typ, string(keyDoc), string(want), body)
		return e
	})
	return recorded, err
}

func recordIfChanged(ctx context.Context, tx pgx.Tx, typ, keyDoc, want string, body map[string]any) (bool, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(742104, hashtext($1))`, typ+"|"+keyDoc); err != nil {
		return false, err
	}
	var last string
	err := tx.QueryRow(ctx, `SELECT body_json::text FROM agentrt.event WHERE event_type = $1 AND body_json @> $2::jsonb
	  ORDER BY event_id DESC LIMIT 1`, typ, keyDoc).Scan(&last)
	hook("seen", typ)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return false, err
	default:
		v, err := canon.Parse([]byte(last))
		if err != nil {
			return false, err
		}
		had, err := canon.Encode(v)
		if err != nil {
			return false, err
		}
		if string(had) == want {
			return false, nil
		}
	}
	if _, err := Append(ctx, tx, Event{Stream: ExternalStream, Type: typ, Actor: "controller", Body: body}); err != nil {
		return false, err
	}
	return true, nil
}

// SeeApproval records an approval's status (approval_seen).
func SeeApproval(ctx context.Context, db DB, number int64, status string) (bool, error) {
	return seen(ctx, db, "approval_seen", map[string]any{"approval": number},
		map[string]any{"approval": number, "status": status})
}

// Question is a question as read from its store. An answered one carries the
// option, the text, who answered and when.
type Question struct {
	ID, Status, Option, Text, AnsweredBy, AnsweredAt string
}

// SeeQuestion records a question's status and, once answered, the answer
// (question_seen).
func SeeQuestion(ctx context.Context, db DB, q Question) (bool, error) {
	body := map[string]any{"question": q.ID, "status": q.Status}
	for k, v := range map[string]string{"option": q.Option, "text": q.Text, "answered_by": q.AnsweredBy, "answered_at": q.AnsweredAt} {
		if v != "" {
			body[k] = v
		}
	}
	return seen(ctx, db, "question_seen", map[string]any{"question": q.ID}, body)
}

// SeeCI records the status of the CI run of a commit (ci_seen).
func SeeCI(ctx context.Context, db DB, repo, commit, status, run string) (bool, error) {
	body := map[string]any{"repo": repo, "commit": commit, "status": status}
	if run != "" {
		body["run"] = run
	}
	return seen(ctx, db, "ci_seen", map[string]any{"repo": repo, "commit": commit}, body)
}

// SeeDeploy records what an environment runs now (deploy_seen): the commit
// and the deploy reference (delivery run and image digests).
func SeeDeploy(ctx context.Context, db DB, env, commit string, ref map[string]any) (bool, error) {
	return seen(ctx, db, "deploy_seen", map[string]any{"env": env},
		map[string]any{"env": env, "commit": commit, "deploy_ref": ref})
}

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
	root, err := stepRoot(ctx, tx, stepID)
	if err != nil {
		return err
	}
	_, err = Append(ctx, tx, Event{Stream: PlanStream(root), Type: "owner_signoff", Actor: actor, PlanNode: stepID,
		Body: map[string]any{"ref": ref}})
	return err
}

// RecordManual records a manual atom of a step as done: a note on the step
// naming the atom and the evidence for it, which must be stored here.
func RecordManual(ctx context.Context, tx pgx.Tx, actor, stepID, manual, text string, evidence []string) error {
	if len(evidence) == 0 {
		return fmt.Errorf("%w: a manual record needs evidence", ErrState)
	}
	if err := haveArtifacts(ctx, tx, evidence); err != nil {
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

// Deployed reads what an environment runs now: its commit and its deploy
// reference. ok is false when the delivery record cannot be read.
type Deployed func(ctx context.Context, env string) (commit string, ref map[string]any, ok bool)

// stepFacts answers a step's atoms from its own records only (v5 §5.1.2
// 「只能引用绑在这一步上的对象」). A database error is kept and ends the
// evaluation without a state change.
type stepFacts struct {
	ctx      context.Context
	tx       pgx.Tx
	step     string
	deployed Deployed
	err      error
}

func (f *stepFacts) exists(q string, args ...any) bool {
	if f.err != nil {
		return false
	}
	var ok bool
	f.err = f.tx.QueryRow(f.ctx, q, args...).Scan(&ok)
	return ok && f.err == nil
}

func (f *stepFacts) TaskAccepted(taskID string) bool {
	return f.exists(`SELECT EXISTS (SELECT 1 FROM agentrt.task WHERE task_id = $1 AND plan_node_id = $2 AND state = 'accepted')`,
		taskID, f.step)
}

func (f *stepFacts) ApprovalExecuted(number int64) bool {
	return f.exists(`SELECT EXISTS (SELECT 1 FROM agentrt.plan_node_binding WHERE plan_node_id = $1 AND bound_kind = 'approval' AND bound_ref = $2)
	  AND EXISTS (SELECT 1 FROM agentrt.event WHERE event_type = 'approval_seen'
	              AND body_json @> jsonb_build_object('approval', $3::bigint, 'status', $4::text))`,
		f.step, strconv.FormatInt(number, 10), number, ApprovalExecuted)
}

func (f *stepFacts) QuestionAnswered(question, option string) bool {
	return f.exists(`SELECT EXISTS (SELECT 1 FROM agentrt.plan_node_binding WHERE plan_node_id = $1 AND bound_kind = 'question' AND bound_ref = $2)
	  AND EXISTS (SELECT 1 FROM agentrt.event WHERE event_type = 'question_seen'
	              AND body_json @> jsonb_build_object('question', $2::text, 'status', $3::text, 'option', $4::text))`,
		f.step, question, QuestionAnswered, option)
}

func (f *stepFacts) CISucceeded(repo, commit string) bool {
	return f.exists(`SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE event_type = 'ci_seen'
	  AND body_json @> jsonb_build_object('repo', $1::text, 'commit', $2::text, 'status', $3::text))`, repo, commit, CISucceeded)
}

func (f *stepFacts) OwnerSignoff(ref string) bool {
	return f.exists(`SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE event_type = 'owner_signoff' AND plan_node_id = $1
	  AND body_json @> jsonb_build_object('ref', $2::text))`, f.step, ref)
}

func (f *stepFacts) ManualRecorded(note string) bool {
	return f.exists(`SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE event_type = 'note' AND plan_node_id = $1
	  AND body_json @> jsonb_build_object('manual', $2::text) AND body_json ? 'evidence')`, f.step, note)
}

// GatePassed is the standing condition: a passing gate verification of this
// test and spec whose commit and deploy reference are what env runs now, and
// whose attempt belongs to its task. What env runs now comes from the
// delivery record at evaluation time; unreadable is unknown.
func (f *stepFacts) GatePassed(env, test, specSHA256 string) plan.Tri {
	if f.err != nil {
		return plan.Unknown
	}
	commit, ref, ok := f.deployed(f.ctx, env)
	if !ok {
		return plan.Unknown
	}
	refDoc, err := canon.Encode(ref)
	if err != nil {
		return plan.Unknown
	}
	if f.exists(`SELECT EXISTS (SELECT 1 FROM agentrt.verification v
	  JOIN agentrt.attempt a ON a.attempt_id = v.attempt_id AND a.task_id = v.task_id
	  WHERE v.method = 'gate' AND v.check_ref = $1 AND v.verdict = 'pass' AND v.env = $2 AND v.subject_commit = $3
	    AND v.deploy_ref_json = $4::jsonb AND v.spec_sha256 = $5)`, test, env, commit, string(refDoc), specSHA256) {
		return plan.True
	}
	if f.err != nil {
		return plan.Unknown
	}
	return plan.False
}

// EvaluateStep evaluates a step's done rule and moves the step when the rule
// says so: planned or active to done when it holds; done back to active
// (standing_lapsed) when a standing condition no longer holds. An unknown
// result moves nothing. It returns the state it moved to, or "".
func EvaluateStep(ctx context.Context, db DB, stepID string, deployed Deployed) (string, error) {
	var moved string
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		var state, doc string
		if err := tx.QueryRow(ctx, `SELECT state, done_rule_json::text FROM agentrt.plan_node
		  WHERE plan_node_id = $1 AND node_kind = 'step' FOR UPDATE`, stepID).Scan(&state, &doc); err != nil {
			return err
		}
		rule, err := plan.Parse([]byte(doc))
		if err != nil {
			return fmt.Errorf("store: step %s: %w", stepID, err)
		}
		if rule.Empty() || state == "dropped" || state == "stopped" {
			return nil
		}
		f := &stepFacts{ctx: ctx, tx: tx, step: stepID, deployed: deployed}
		res := rule.Eval(f)
		if f.err != nil {
			return f.err
		}
		to, cause := "", ""
		switch {
		case res.Value == plan.True && (state == "planned" || state == "active"):
			to, cause = "done", "done_rule"
			if res.Manual {
				cause = "done_rule_manual"
			}
		case res.Value == plan.False && state == "done" && rule.Standing():
			to, cause = "active", "standing_lapsed"
		default:
			return nil
		}
		root, err := stepRoot(ctx, tx, stepID)
		if err != nil {
			return err
		}
		if _, err := SetStepState(ctx, tx, "controller", root, stepID, to, cause); err != nil {
			return err
		}
		moved = to
		return nil
	})
	return moved, err
}
