package store

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/gaterun"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planimport"
)

// Plan changes and the gates (TD-397, CUT §1, §3, §7②).
//
//   - gateGuard refuses, wherever a change is tried or applied, a step bound
//     to a gate batch (add_node, rebind_step), a gate member moved
//     (move_task), and a done rule naming a gate whose spec is not a stored
//     gate spec: a batch stays the controller's alone (review td397-2 A3,
//     td397-3 B-a), and a gate that could never run is refused when it is
//     written.
//   - gatesRecommend is a plan-change card's recommendation: apply, when
//     every change gives a distinct architecture row with no rule yet,
//     without bindings, exactly the done rule the reviewed catalog
//     (gaterun.Catalog) gives that row in this environment, every spec
//     stored. Anything else recommends nothing. On STG the card-auto-answer
//     rule answers with what a card recommends; PROD shows it and the Owner
//     answers (CUT §7② A, the design's recommendation, which the
//     coordinating thread is to confirm).
//   - gatesStillRecommended: a change the rule answered for is applied only
//     while it is still what gatesRecommend recommends (a row given a rule
//     meanwhile is not overwritten).

// runtimeEnv is the environment of the database's runtime role
// (platform_agentrt_<env>, migrate.RuntimeRole; each environment's runtime
// reaches its own database only, T19), or "" for any other role.
func runtimeEnv(ctx context.Context, tx pgx.Tx) (string, error) {
	var user string
	if err := tx.QueryRow(ctx, `SELECT current_user`).Scan(&user); err != nil {
		return "", err
	}
	env := strings.TrimPrefix(user, "platform_agentrt_")
	if env == user || !envNames[env] {
		return "", nil
	}
	return env, nil
}

// gateGuard is applyChanges's check of one change against the gate batches
// and the gate specs.
func gateGuard(ctx context.Context, tx pgx.Tx, c PlanChange) error {
	switch {
	case (c.Op == "add_node" && c.Kind == "step" && c.MissionID != "") || c.Op == "rebind_step":
		var gate bool
		err := tx.QueryRow(ctx, `SELECT scope_json ? 'gate_run' FROM agentrt.mission WHERE mission_id = $1`, c.MissionID).Scan(&gate)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if gate {
			return invalid("mission %s is a batch of gates: no step is bound to it", c.MissionID)
		}
	case c.Op == "move_task":
		member, err := isGateMember(ctx, tx, c.Task)
		if err != nil {
			return err
		}
		if member {
			return invalid("task %s is a gate of its batch: it does not move", c.Task)
		}
	case c.Op == "set_done_rule":
		rule, err := plan.Parse(c.Rule)
		if err != nil {
			return nil // checkChanges refuses it
		}
		for _, g := range rule.Gates() {
			if err = storedGateSpec(ctx, tx, g.Spec); err != nil {
				return invalid("%s: gate %s: %v", c.Node, g.Test, err)
			}
		}
	}
	return nil
}

// gatesRecommend is the recommendation of a plan change's card (1, apply,
// with why) or none (0).
func gatesRecommend(ctx context.Context, tx pgx.Tx, root string, changes []PlanChange) (int, string, error) {
	if root != planimport.ArchRootID || len(changes) == 0 {
		return 0, "", nil
	}
	env, err := runtimeEnv(ctx, tx)
	if err != nil || env == "" {
		return 0, "", err
	}
	seen := map[string]bool{}
	for _, c := range changes {
		row, inCatalog := gaterun.RowFor(c.Node)
		if c.Op != "set_done_rule" || len(c.Bind) > 0 || seen[c.Node] || !inCatalog {
			return 0, "", nil
		}
		seen[c.Node] = true
		var kind, parent, doc string
		err := tx.QueryRow(ctx, `SELECT node_kind, COALESCE(parent_plan_node_id, ''), done_rule_json::text FROM agentrt.plan_node
		  WHERE plan_node_id = $1`, c.Node).Scan(&kind, &parent, &doc)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, "", nil
		}
		if err != nil {
			return 0, "", err
		}
		empty, err := plan.Parse([]byte(doc))
		if err != nil || kind != "arch_row" || parent != planimport.ArchRootID || !empty.Empty() {
			return 0, "", nil
		}
		want, err := row.Rule(env)
		if err != nil {
			return 0, "", err
		}
		got, err := canonRule(c.Rule)
		if err != nil || string(got) != string(want) {
			return 0, "", nil
		}
		rule, err := plan.Parse(got)
		if err != nil {
			return 0, "", nil
		}
		for _, g := range rule.Gates() {
			if storedGateSpec(ctx, tx, g.Spec) != nil {
				return 0, "", nil
			}
		}
	}
	return 1, "it gives architecture rows their first gates, exactly the reviewed catalog's for " + env, nil
}

// gatesStillRecommended refuses applying, on a rule's answer, a change that
// names an architecture row's gates and is no longer what gatesRecommend
// recommends. The changes' nodes are locked (lockChanges).
func gatesStillRecommended(ctx context.Context, tx pgx.Tx, root string, changes []PlanChange) error {
	names := false
	for _, c := range changes {
		if c.Op == "set_done_rule" {
			if r, err := plan.Parse(c.Rule); err == nil && len(r.Gates()) > 0 {
				names = true
			}
		}
	}
	if !names {
		return nil
	}
	rec, _, err := gatesRecommend(ctx, tx, root, changes)
	if err != nil || rec == 1 {
		return err
	}
	return invalid("a rule answered this change, and it is no longer the catalog's first gates for its rows")
}
