package store

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
)

func validatePlanBinding(ctx context.Context, tx pgx.Tx, b planview.Binding, mapping map[string]string) error {
	var parent, kind string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(parent_plan_node_id,''),node_kind FROM agentrt.plan_node WHERE plan_node_id=$1`, b.Step).Scan(&parent, &kind); err != nil {
		return err
	}
	if kind != "step" || (mapping[b.Node] != b.Step && mapping[b.Node] != parent) {
		return invalid("binding %s is not on this step", b.ID)
	}
	if b.Task != "" {
		var valid bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agentrt.task t JOIN agentrt.mission m USING(mission_id) JOIN agentrt.attempt a USING(task_id) WHERE t.task_id=$1 AND a.attempt_id=$2 AND t.mission_id=$3 AND t.plan_node_id=$4 AND m.tenant='dev' AND t.repo=$5 AND a.pushed_commit=$6 AND t.round_no=$7)`, b.Task, b.Attempt, b.Mission, b.Step, b.Repo, b.Result, b.Round).Scan(&valid)
		if err != nil {
			return err
		}
		if !valid {
			return invalid("binding runtime identity mismatch")
		}
	}
	if b.Verification != "" {
		var valid bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agentrt.verification WHERE verification_id=$1 AND task_id=$2 AND attempt_id=$3 AND subject_commit=$4)`, b.Verification, b.Task, b.Attempt, b.Result).Scan(&valid)
		if err != nil {
			return err
		}
		if !valid {
			return invalid("verification is not for bound task/attempt")
		}
	}
	if b.ExternalWork != "" {
		n, err := bindingExternal(ctx, tx, b.ExternalWork)
		if err != nil {
			return err
		}
		if !externalMatches(n, b) {
			return invalid("external submission identity mismatch")
		}
	}
	return nil
}
func bindingExternal(ctx context.Context, tx pgx.Tx, id string) (externalNote, error) {
	var raw []byte
	var n externalNote
	err := tx.QueryRow(ctx, `SELECT body_json->'external_work' FROM agentrt.event WHERE event_type='note' AND body_json->'external_work'->>'external_work_id'=$1 ORDER BY seq DESC LIMIT 1`, id).Scan(&raw)
	if err == nil {
		err = json.Unmarshal(raw, &n)
	}
	return n, err
}
func externalMatches(n externalNote, b planview.Binding) bool {
	return n.Root == b.Root && n.Node == b.Step && n.State == "Submitted" && n.Seq == b.ExternalSeq && n.ReportDigest == b.Report && (b.Task == "" || n.Task == b.Task && n.Mission == b.Mission && slices.Contains(n.Commits, b.Result))
}

// Adoption is the existing plan decision, not a new completion write. It
// installs conditions only; the next controller pass decides their truth.
// A step's other conditions stay: the atoms an earlier version bound are
// replaced by this version's, under one "all" with the rest of the rule; a
// step this version binds nothing on loses its old atoms (PV2 review
// 2026-10-26: adoption replaced a step's Owner signoff and gates).
func installPlanBindings(ctx context.Context, tx pgx.Tx, d *planview.Document, mapping map[string]string, actor string, decision int64) error {
	bindings, err := d.Bindings()
	if err != nil {
		return err
	}
	byStep := map[string][]any{}
	for _, b := range bindings {
		if err = validatePlanBinding(ctx, tx, b, mapping); err != nil {
			return err
		}
		byStep[b.Step] = append(byStep[b.Step], map[string]any{b.Atom: map[string]any{"binding": b.ID, "version": b.Version}})
	}
	rows, err := tx.Query(ctx, `SELECT s.plan_node_id,s.done_rule_json FROM agentrt.plan_node s JOIN agentrt.plan_node i ON i.plan_node_id=s.parent_plan_node_id WHERE s.node_kind='step' AND i.parent_plan_node_id=$1 ORDER BY s.plan_node_id FOR UPDATE OF s`, d.Root)
	if err != nil {
		return err
	}
	current := map[string][]byte{}
	var order []string
	for rows.Next() {
		var id string
		var raw []byte
		if err = rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return err
		}
		current[id] = raw
		order = append(order, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	rules := map[string][]byte{}
	for _, step := range order {
		next, e := composeBoundRule(current[step], byStep[step])
		if e != nil {
			return invalid("step %s: %v", step, e)
		}
		was, e := canonRule(current[step])
		if e != nil {
			return e
		}
		if string(next) != string(was) {
			rules[step] = next
		}
	}
	for step := range byStep {
		if _, ok := current[step]; !ok {
			return invalid("binding step %s is not a step of %s", step, d.Root)
		}
	}
	if len(rules) == 0 && len(byStep) == 0 {
		return nil
	}
	records := []any{}
	for _, b := range bindings {
		record, e := jsonObject(b)
		if e != nil {
			return e
		}
		records = append(records, record)
	}
	if _, err = Append(ctx, tx, Event{Stream: PlanStream(d.Root), Type: "plan_changed", Actor: actor, Cause: decision, Body: map[string]any{"change": "adopt_plan_bindings", "decided_event_id": decision, "bindings": records}}); err != nil {
		return err
	}
	for _, step := range order {
		raw, ok := rules[step]
		if !ok {
			continue
		}
		if _, err = tx.Exec(ctx, `UPDATE agentrt.plan_node SET done_rule_json=$2::jsonb,version=version+1 WHERE plan_node_id=$1`, step, raw); err != nil {
			return err
		}
	}
	return nil
}

// adoptsBindings says whether changes adopt a view that binds completion atoms.
func adoptsBindings(ctx context.Context, tx pgx.Tx, changes []PlanChange) (bool, error) {
	for _, c := range changes {
		if c.Op != "adopt_plan_view" {
			continue
		}
		doc, err := viewArtifact(ctx, tx, c.Artifact)
		if err != nil {
			return false, err
		}
		bs, err := doc.Bindings()
		if err != nil || len(bs) > 0 {
			return true, err
		}
	}
	return false, nil
}

var boundAtoms = []string{plan.AtomMain, plan.AtomDeployment, plan.AtomReview, plan.AtomExternal}

// composeBoundRule is old with its bound atoms removed, and atoms added under
// one "all". A bound atom under "any" is refused: removing it would change
// what the rest of that "any" means.
func composeBoundRule(old []byte, atoms []any) ([]byte, error) {
	v, err := canon.Parse(old)
	if err != nil {
		return nil, err
	}
	obj, _ := v.(map[string]any)
	var base map[string]any
	if len(obj) > 0 {
		kept, keep, e := stripBound(obj, false)
		if e != nil {
			return nil, e
		}
		if keep {
			base = kept
		}
	}
	var out map[string]any
	switch {
	case len(atoms) == 0 && base == nil:
		out = map[string]any{}
	case len(atoms) == 0:
		out = base
	case base == nil:
		out = map[string]any{"all": atoms}
	default:
		children := []any{base}
		if all, ok := base["all"].([]any); ok && len(base) == 1 {
			children = all
		}
		out = map[string]any{"all": append(append([]any{}, children...), atoms...)}
	}
	raw, err := canon.Encode(out)
	if err != nil {
		return nil, err
	}
	if _, err = plan.Parse(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func stripBound(n map[string]any, underAny bool) (map[string]any, bool, error) {
	if len(n) == 1 {
		for k, v := range n {
			if slices.Contains(boundAtoms, k) {
				if underAny {
					return nil, false, errors.New("a bound atom under any cannot be replaced; change the rule by a plan change first")
				}
				return nil, false, nil
			}
			if k == "all" || k == "any" {
				list, _ := v.([]any)
				kept := []any{}
				for _, c := range list {
					co, ok := c.(map[string]any)
					if !ok {
						kept = append(kept, c)
						continue
					}
					x, keep, e := stripBound(co, underAny || k == "any")
					if e != nil {
						return nil, false, e
					}
					if keep {
						kept = append(kept, x)
					}
				}
				if len(kept) == 0 {
					return nil, false, nil
				}
				return map[string]any{k: kept}, true, nil
			}
		}
	}
	return n, true, nil
}

// Other rule writers may only reference a binding actually adopted for this
// exact step. A version switch cannot silently point at another acceptance.
func checkBoundRule(ctx context.Context, tx pgx.Tx, step string, r plan.Rule) error {
	if len(r.BoundRefs()) == 0 {
		return nil
	}
	snap, err := readPlanFacts(ctx, tx)
	if err != nil {
		return err
	}
	for _, ref := range r.BoundRefs() {
		if _, ok := snap.find(step, ref.Atom, ref.Binding, ref.Version); !ok {
			return invalid("unadopted or cross-step binding %s", ref.Binding)
		}
	}
	return nil
}
