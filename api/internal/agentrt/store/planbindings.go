package store

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/jackc/pgx/v5"
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
	if len(b.Evidence) > 0 {
		return haveArtifacts(ctx, tx, b.Evidence)
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
	if len(byStep) > 0 {
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
	}
	for step, atoms := range byStep {
		raw, err := json.Marshal(map[string]any{"all": atoms})
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE agentrt.plan_node SET done_rule_json=$2::jsonb,version=version+1 WHERE plan_node_id=$1`, step, raw); err != nil {
			return err
		}
	}
	return nil
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
