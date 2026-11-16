package planreadings

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
)

// TreeItem and TreeRoot retain the database ordering of the progress tree.
// They are observations; architecture completion rules are evaluated by the caller.
type TreeItem struct {
	ID, Label, Title string
	Steps            []plan.Step
	Active           []string
}
type TreeRoot struct {
	ID, Kind, Title string
	Items           []*TreeItem
}

// ProgressTree reads roots, their items and steps in the caller's snapshot.
// It never evaluates evidence, gates or permissions.
func ProgressTree(ctx context.Context, tx pgx.Tx) ([]*TreeRoot, error) {
	rows, err := tx.Query(ctx, `SELECT r.plan_node_id, r.node_kind, r.title, i.plan_node_id, i.label, i.title,
	    s.plan_node_id, s.state, COALESCE(e.body_json->>'cause', '')
	  FROM agentrt.plan_node r
	  LEFT JOIN agentrt.plan_node i ON i.parent_plan_node_id = r.plan_node_id AND i.node_kind IN ('item','arch_row')
	  LEFT JOIN agentrt.plan_node s ON s.parent_plan_node_id = i.plan_node_id AND s.node_kind = 'step'
	  LEFT JOIN agentrt.event e ON e.event_id = s.state_event_id
	  WHERE r.parent_plan_node_id IS NULL
	  ORDER BY r.node_kind, r.ordinal, r.plan_node_id, i.ordinal, s.ordinal`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var roots []*TreeRoot
	byRoot := map[string]*TreeRoot{}
	byItem := map[string]*TreeItem{}
	for rows.Next() {
		var rid, rkind, rtitle string
		var iid, ilabel, ititle, sid, sstate *string
		var cause string
		if err = rows.Scan(&rid, &rkind, &rtitle, &iid, &ilabel, &ititle, &sid, &sstate, &cause); err != nil {
			return nil, err
		}
		r := byRoot[rid]
		if r == nil {
			r = &TreeRoot{ID: rid, Kind: rkind, Title: rtitle}
			byRoot[rid] = r
			roots = append(roots, r)
		}
		if iid == nil {
			continue
		}
		it := byItem[*iid]
		if it == nil {
			it = &TreeItem{ID: *iid, Label: *ilabel, Title: *ititle}
			byItem[*iid] = it
			r.Items = append(r.Items, it)
		}
		if sid == nil || sstate == nil {
			continue
		}
		it.Steps = append(it.Steps, plan.Step{ID: *sid, State: *sstate, Manual: *sstate == plan.StateDone && cause == "done_rule_manual"})
		if *sstate == plan.StateActive {
			it.Active = append(it.Active, *sid)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	return roots, nil
}
