package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
)

// PlanView reads every source from one read-only repeatable-read snapshot.
// Versioned observations, dependencies and freshness share that snapshot.
// Neither a legacy tick nor an external submission proves completion.
func PlanView(ctx context.Context, db DB, root string) (map[string]any, error) {
	return PlanViewPage(ctx, db, root, "")
}

func PlanViewPage(ctx context.Context, db DB, root, cursor string) (map[string]any, error) {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT transaction_timestamp()`).Scan(&now); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, viewTree+`SELECT to_jsonb(n) FROM tree JOIN agentrt.plan_node n USING(plan_node_id) ORDER BY n.ordinal,n.plan_node_id LIMIT 501`, root)
	if err != nil {
		return nil, err
	}
	nodes, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (map[string]any, error) {
		var b []byte
		var n map[string]any
		err := r.Scan(&b)
		if err == nil {
			err = json.Unmarshal(b, &n)
		}
		return n, err
	})
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, pgx.ErrNoRows
	}
	if len(nodes) > 500 {
		return nil, invalid("plan exceeds 500 nodes")
	}
	views, err := adoptedViews(ctx, tx)
	if err != nil {
		return nil, err
	}
	adoption := views[root]
	graph := planview.Graph{Mainline: []planview.NodeRef{}, Edges: []planview.Dependency{}}
	docs := map[string]*planview.Document{}
	items := []planview.Item{}
	warnings := []string{}
	if adoption.Artifact != "" {
		for r, a := range views {
			docs[r], err = viewArtifact(ctx, tx, a.Artifact)
			if err != nil {
				return nil, err
			}
		}
		graph, err = planview.Resolve(root, docs)
		if err != nil {
			return nil, err
		}
		items = docs[root].Items
	} else {
		warnings = append(warnings, "Legacy evidence: plan has no adopted view; nodes are Unclassified")
	}
	byID := map[string]map[string]any{}
	for _, n := range nodes {
		n["lane_kind"] = "Unclassified"
		byID[n["plan_node_id"].(string)] = n
	}
	for _, item := range items {
		if n := byID[adoption.Nodes[item.Node]]; n != nil {
			n["lane_kind"] = item.Lane
			n["view"] = item
		}
	}
	snap, err := readPlanFacts(ctx, tx)
	if err != nil {
		return nil, err
	}
	candidates, err := planEvidenceCandidates(ctx, tx, snap, root)
	if err != nil {
		return nil, err
	}
	mainline, next, waiting, acceptanceReady, ownerLater, verificationFreshness := verificationView(snap, root, nodes, graph, adoption, candidates)
	ext, err := snapshotExternal(ctx, tx, root, now)
	if err != nil {
		return nil, err
	}
	live, err := liveAttempts(ctx, tx, now, time.Time{})
	if err != nil {
		return nil, err
	}
	tasksRaw := []byte{}
	err = tx.QueryRow(ctx, viewTree+`SELECT COALESCE(jsonb_agg(jsonb_build_object('task_id',t.task_id,'plan_node_id',t.plan_node_id,'mission_id',t.mission_id,'title',t.title,'state',t.state) ORDER BY t.task_id),'[]') FROM agentrt.task t JOIN tree USING(plan_node_id)`, root).Scan(&tasksRaw)
	if err != nil {
		return nil, err
	}
	var tasks []map[string]any
	if err = json.Unmarshal(tasksRaw, &tasks); err != nil {
		return nil, err
	}
	working := []any{}
	ownerWaiting := acceptanceReady
	finished := []any{}
	linked := map[string][]map[string]any{}
	for _, e := range ext {
		e["verification"] = snap.externalVerification(e["external_work_id"].(string))
		task, _ := e["task_id"].(string)
		if task != "" {
			linked[task] = append(linked[task], e)
		}
		// An active runtime attempt owns the current execution facts. Keep
		// external observations as its history, regardless of their state.
		if live[task] == nil && (e["state"] == "Running" || e["state"] == "Registered") {
			working = append(working, e)
		}
		if end, _ := e["ended_at"].(string); len(end) >= 10 && end[:10] == now.UTC().Format("2006-01-02") {
			finished = append(finished, e)
		}
	}
	for _, t := range tasks {
		if l := snap.taskLanding(t["task_id"].(string)); l != nil {
			t["landing"] = l
		}
		if t["state"] == "accepted" {
			t["delivery"] = "Accepted · " + snap.taskDelivery(t["task_id"].(string))
		}
		if t["state"] == "awaiting_owner" {
			ownerWaiting = append(ownerWaiting, map[string]any{"task_id": t["task_id"], "plan_node_id": t["plan_node_id"], "reason": "Runtime task awaits Owner", "source": "agentrt.task"})
		}
		if a := live[t["task_id"].(string)]; a != nil {
			t["attempt"] = a
			if history := linked[a.TaskID]; len(history) > 0 {
				t["external_work"] = history
			}
			working = append(working, t)
		}
	}
	var doneRaw []byte
	err = tx.QueryRow(ctx, viewTree+`SELECT COALESCE(jsonb_agg(jsonb_build_object('attempt_id',a.attempt_id,'task_id',a.task_id,'state',a.state,'ended_at',a.ended_at,'meaning','Execution ended; not plan completion')),'[]') FROM agentrt.attempt a JOIN agentrt.task t USING(task_id) JOIN tree USING(plan_node_id) WHERE a.ended_at >= date_trunc('day',$2::timestamptz AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AND a.ended_at <= $2`, root, now).Scan(&doneRaw)
	if err != nil {
		return nil, err
	}
	var ended []any
	if err = json.Unmarshal(doneRaw, &ended); err != nil {
		return nil, err
	}
	finished = append(finished, ended...)
	var unregistered int
	err = tx.QueryRow(ctx, viewTree+`SELECT count(DISTINCT e.actor_id) FROM agentrt.event e JOIN tree ON tree.plan_node_id=e.plan_node_id WHERE e.actor_id LIKE 'thread:%' AND e.at >= $2::timestamptz-interval '24 hours' AND NOT EXISTS(SELECT 1 FROM agentrt.event x WHERE x.event_type='note' AND x.body_json->'external_work'->>'registrant'=e.actor_id AND x.body_json->'external_work'->>'root'=$1)`, root, now).Scan(&unregistered)
	if err != nil {
		return nil, err
	}
	watermarks := map[string]int64{}
	rows, err = tx.Query(ctx, `SELECT event_stream_id,last_seq FROM agentrt.event_stream ORDER BY event_stream_id`)
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
	recent, nextRecent, recentAsOf, err := planViewRecent(ctx, tx, root, cursor, now)
	if err != nil {
		return nil, err
	}
	if verificationFreshness == "Unknown" {
		warnings = append(warnings, "Acceptance verification requires adopted bindings and trusted evidence")
	}
	return map[string]any{"schema": "agentrt.plan_view_response/v1", "root": root, "plan_version": adoption.Version, "artifact_id": adoption.Artifact, "as_of": timeText(now), "nodes": nodes, "items": items, "edges": graph.Edges, "mainline": mainline, "next": next, "waiting_on": waiting, "owner_waiting": ownerWaiting, "owner_later": ownerLater, "working": working, "finished_today": finished, "day_timezone": "UTC", "tasks": tasks, "external_work": ext, "coverage": map[string]any{"unregistered_work": unregistered, "label": "Unregistered work", "scope": "Threads with plan-node events in this root in the last 24 hours; unsupported processes are not observable", "reconciled": false}, "source_watermarks": watermarks, "freshness": map[string]any{"database": map[string]any{"status": "Fresh", "observed_at": timeText(now)}, "external_work": "Per-record server heartbeat; Unconfirmed after 60 seconds", "verification": verificationFreshness, "owner_waiting": "Runtime task waits and versioned acceptance signoffs; legacy readiness remains Unknown"}, "eta": map[string]any{"status": "Unknown", "reason": "Capacity and duration observations are not available"}, "recent_changes": json.RawMessage(recent), "recent_next": nextRecent, "recent_as_of": recentAsOf, "warnings": warnings}, nil
}
func itemIndex(d *planview.Document, node string) int {
	for i, n := range d.Items {
		if n.Node == node {
			return i
		}
	}
	return 0
}
func snapshotExternal(ctx context.Context, tx pgx.Tx, root string, now time.Time) ([]map[string]any, error) {
	rows, err := tx.Query(ctx, `SELECT body,at FROM (SELECT DISTINCT ON(event_stream_id) body_json->'external_work' AS body,at FROM agentrt.event WHERE event_type='note' AND body_json ? 'external_work' ORDER BY event_stream_id,seq DESC) x WHERE body->>'root'=$1 ORDER BY body->>'external_work_id'`, root)
	if err != nil {
		return nil, err
	}
	type record struct {
		n  externalNote
		at time.Time
	}
	rs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (record, error) {
		var x record
		var raw []byte
		err := r.Scan(&raw, &x.at)
		if err == nil {
			err = json.Unmarshal(raw, &x.n)
		}
		return x, err
	})
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, r := range rs {
		raw, _ := json.Marshal(r.n)
		var m map[string]any
		if err = json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}
		d, err := externalDescription(ctx, tx, r.n.Artifact)
		if err != nil {
			return nil, err
		}
		m["description"] = d
		m["recorded_at"] = timeText(r.at)
		hb, _ := time.Parse(time.RFC3339Nano, r.n.HeartbeatAt)
		m["heartbeat_age_s"] = int64(now.Sub(hb).Seconds())
		m["status"] = r.n.State
		if (r.n.State == "Running" || r.n.State == "Registered") && now.Sub(hb) > 60*time.Second {
			m["status"] = "Unconfirmed"
		}
		start, _ := time.Parse(time.RFC3339Nano, r.n.StartedAt)
		end := now
		if r.n.EndedAt != "" {
			end, _ = time.Parse(time.RFC3339Nano, r.n.EndedAt)
		}
		if !start.IsZero() {
			m["elapsed_s"] = max(int64(0), int64(end.Sub(start).Seconds()))
		}
		m["backfilled"] = r.n.SourceStartedAt != ""
		out = append(out, m)
	}
	return out, nil
}
