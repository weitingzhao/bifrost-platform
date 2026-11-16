package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/workregistration"
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
	snap, err := readPlanFacts(ctx, tx)
	if err != nil {
		return nil, err
	}
	adoption := snap.Views[root]
	graph := planview.Graph{Mainline: []planview.NodeRef{}, Edges: []planview.Dependency{}}
	items := []planview.Item{}
	warnings := []string{}
	if warning := snap.Warnings[root]; warning != "" {
		warnings = append(warnings, warning)
	} else if doc := snap.Docs[root]; doc != nil {
		graph = adoptedViewGraph(doc)
		items = doc.Items
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
	candidates, err := planEvidenceCandidates(ctx, tx, snap, root)
	if err != nil {
		return nil, err
	}
	ext, err := snapshotExternal(ctx, tx, root, now)
	if err != nil {
		return nil, err
	}
	mainline, next, waiting, acceptanceReady, ownerLater, verificationFreshness := verificationView(snap, root, nodes, graph, adoption, candidates)
	working := []any{}
	ownerWaiting := acceptanceReady
	finished := []any{}
	for _, e := range ext {
		e["verification"] = snapshotExternalVerification(snap, e["external_work_id"].(string))
		if e["state"] == "Running" || e["state"] == "Registered" {
			working = append(working, e)
		}
		if end, _ := e["ended_at"].(string); len(end) >= 10 && end[:10] == now.UTC().Format("2006-01-02") {
			finished = append(finished, e)
		}
	}
	// Threads only: a host's collector (thread:collector@<host>, R5:5.5-12
	// §1.1, §3.1) is a thread by its actor's kind, not a session, and is
	// never unregistered work.
	var unregistered int
	err = tx.QueryRow(ctx, viewTree+`SELECT count(DISTINCT e.actor_id) FROM agentrt.event e JOIN tree ON tree.plan_node_id=e.plan_node_id WHERE e.actor_id LIKE 'thread:%' AND e.actor_id NOT LIKE '`+CollectorActorPrefix+`%' AND e.at >= $2::timestamptz-interval '24 hours' AND NOT EXISTS(SELECT 1 FROM agentrt.event x WHERE x.event_type='note' AND x.body_json->'external_work'->>'registrant'=e.actor_id AND x.body_json->'external_work'->>'root'=$1)`, root, now).Scan(&unregistered)
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
	return map[string]any{"schema": "agentrt.plan_view_response/v1", "root": root, "plan_version": adoption.Version, "artifact_id": adoption.Artifact, "as_of": timeText(now), "nodes": nodes, "items": items, "edges": graph.Edges, "mainline": mainline, "next": next, "waiting_on": waiting, "owner_waiting": ownerWaiting, "owner_later": ownerLater, "working": working, "finished_today": finished, "day_timezone": "UTC", "external_work": ext, "coverage": map[string]any{"unregistered_work": unregistered, "label": "Unregistered work", "scope": "Threads with plan-node events in this root in the last 24 hours; unsupported processes are not observable", "reconciled": false}, "source_watermarks": watermarks, "freshness": map[string]any{"database": map[string]any{"status": "Fresh", "observed_at": timeText(now)}, "external_work": "Per-record server heartbeat; Unconfirmed after 60 seconds", "verification": verificationFreshness, "owner_waiting": "Versioned acceptance signoffs; legacy readiness remains Unknown"}, "eta": map[string]any{"status": "Unknown", "reason": "Capacity and duration observations are not available"}, "recent_changes": json.RawMessage(recent), "recent_next": nextRecent, "recent_as_of": recentAsOf, "warnings": warnings}, nil
}

// Adapt the persisted view to the domain projection without changing snapshot reads.
func adoptedViewGraph(d *planview.Document) planview.Graph { return planview.AdoptedGraph(d) }
func itemIndex(d *planview.Document, node string) int      { return planview.ItemIndex(d, node) }

func snapshotExternal(ctx context.Context, tx pgx.Tx, root string, now time.Time) ([]map[string]any, error) {
	return workregistration.Snapshot(ctx, tx, root, now, func(ctx context.Context, tx pgx.Tx, id string) (any, error) {
		return externalDescription(ctx, tx, id)
	})
}
