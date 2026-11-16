package planreadings

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
)

const snapshotTree = `WITH RECURSIVE tree AS (SELECT plan_node_id FROM agentrt.plan_node WHERE plan_node_id=$1 UNION ALL SELECT n.plan_node_id FROM agentrt.plan_node n JOIN tree t ON n.parent_plan_node_id=t.plan_node_id) `

// SnapshotNodes reads at most 501 nodes in the caller's snapshot. The caller
// retains the empty-tree and 500-node validation and all evidence decisions.
func SnapshotNodes(ctx context.Context, tx pgx.Tx, root string) ([]map[string]any, error) {
	rows, err := tx.Query(ctx, snapshotTree+`SELECT to_jsonb(n) FROM tree JOIN agentrt.plan_node n USING(plan_node_id) ORDER BY n.ordinal,n.plan_node_id LIMIT 501`, root)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (map[string]any, error) {
		var b []byte
		var n map[string]any
		err := r.Scan(&b)
		if err == nil {
			err = json.Unmarshal(b, &n)
		}
		return n, err
	})
}

// SourceWatermarks observes stream positions in the same transaction as nodes.
func SourceWatermarks(ctx context.Context, tx pgx.Tx) (map[string]int64, error) {
	watermarks := map[string]int64{}
	rows, err := tx.Query(ctx, `SELECT event_stream_id,last_seq FROM agentrt.event_stream ORDER BY event_stream_id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var s string
		var seq int64
		if err = rows.Scan(&s, &seq); err != nil {
			rows.Close()
			return nil, err
		}
		watermarks[s] = seq
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return watermarks, nil
}
