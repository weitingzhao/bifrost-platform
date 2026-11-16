package planreadings

import (
	"context"
	"github.com/jackc/pgx/v5"
	"time"
)

// ArchitectureRules reads stored rule text; it does not parse or evaluate it.
// The caller supplies its projection transaction and the exact row ids.
func ArchitectureRules(ctx context.Context, tx pgx.Tx, ids []string) (map[string]string, error) {
	rules := map[string]string{}
	rrows, err := tx.Query(ctx, `SELECT plan_node_id, done_rule_json::text FROM agentrt.plan_node WHERE plan_node_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	for rrows.Next() {
		var id, doc string
		if err = rrows.Scan(&id, &doc); err != nil {
			rrows.Close()
			return nil, err
		}
		rules[id] = doc
	}
	rrows.Close()
	if err = rrows.Err(); err != nil {
		return nil, err
	}
	return rules, nil
}

// ArchitectureNotes reads the last appended note, not the newest writer timestamp.
func ArchitectureNotes(ctx context.Context, tx pgx.Tx, ids []string) (map[string]map[string]any, error) {
	notes := map[string]map[string]any{}
	nrows, err := tx.Query(ctx, `SELECT DISTINCT ON (plan_node_id) plan_node_id, body_json->>'text', at, actor_id FROM agentrt.event
	  WHERE event_type = 'note' AND plan_node_id = ANY($1) ORDER BY plan_node_id, event_id DESC`, ids)
	if err != nil {
		return nil, err
	}
	for nrows.Next() {
		var id, text, by string
		var at time.Time
		if err = nrows.Scan(&id, &text, &at, &by); err != nil {
			nrows.Close()
			return nil, err
		}
		notes[id] = map[string]any{"text": text, "at": at.UTC().Format(time.RFC3339Nano), "by": by}
	}
	nrows.Close()
	if err = nrows.Err(); err != nil {
		return nil, err
	}
	return notes, nil
}
