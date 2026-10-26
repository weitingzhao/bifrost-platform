package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
)

type viewAdoption struct {
	V        int               `json:"v"`
	Kind     string            `json:"kind"`
	Root     string            `json:"root"`
	Tenant   string            `json:"tenant"`
	Version  int               `json:"plan_version"`
	Artifact string            `json:"artifact_id"`
	Nodes    map[string]string `json:"nodes"`
	Decision int64             `json:"decided_event_id"`
}

func adoptedViews(ctx context.Context, tx pgx.Tx) (map[string]viewAdoption, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (event_stream_id) body_json->'plan_view' FROM agentrt.event WHERE event_type='note' AND body_json ? 'plan_view' ORDER BY event_stream_id, seq DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]viewAdoption{}
	for rows.Next() {
		var b []byte
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		var a viewAdoption
		if err = json.Unmarshal(b, &a); err != nil {
			return nil, err
		}
		out[a.Root] = a
	}
	return out, rows.Err()
}

func viewArtifact(ctx context.Context, tx pgx.Tx, id string) (*planview.Document, error) {
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT b.body FROM agentrt.artifact_blob b JOIN agentrt.artifact a USING(artifact_id) WHERE a.artifact_id=$1 AND a.artifact_kind='other'`, id).Scan(&raw); err != nil {
		return nil, invalid("plan-view artifact unavailable: %v", err)
	}
	doc, err := planview.Parse(raw)
	if err != nil {
		return nil, invalid("plan view: %v", err)
	}
	if doc.Tenant != "dev" {
		return nil, invalid("plan views support only dev")
	}
	return doc, nil
}

// A graph writer serializes with other graph writers before acquiring root
// locks in sorted order. Existing single-root writers take only their root.
// All adopted roots are included: incoming edges must be revalidated too.
func lockPlanViewChanges(ctx context.Context, tx pgx.Tx, root string, cs []PlanChange) error {
	if len(cs) != 1 || cs[0].Op != "adopt_plan_view" {
		return lockTree(ctx, tx, root)
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(742119)`); err != nil {
		return err
	}
	views, err := adoptedViews(ctx, tx)
	if err != nil {
		return err
	}
	roots := []string{root}
	for r := range views {
		roots = append(roots, r)
	}
	slices.Sort(roots)
	roots = slices.Compact(roots)
	for _, r := range roots {
		if err = lockTree(ctx, tx, r); err != nil {
			return err
		}
	}
	return nil
}

const viewTree = `WITH RECURSIVE tree AS (SELECT plan_node_id FROM agentrt.plan_node WHERE plan_node_id=$1 UNION ALL SELECT n.plan_node_id FROM agentrt.plan_node n JOIN tree t ON n.parent_plan_node_id=t.plan_node_id) `

func devTree(ctx context.Context, tx pgx.Tx, root string) error {
	var bad bool
	err := tx.QueryRow(ctx, viewTree+`SELECT EXISTS(SELECT 1 FROM tree JOIN agentrt.plan_node n USING(plan_node_id) JOIN agentrt.mission m USING(mission_id) WHERE m.tenant<>'dev') OR EXISTS(SELECT 1 FROM tree JOIN agentrt.task t USING(plan_node_id) JOIN agentrt.mission m USING(mission_id) WHERE m.tenant<>'dev')`, root).Scan(&bad)
	if err != nil {
		return err
	}
	if bad {
		return invalid("plan %s contains business work", root)
	}
	return nil
}

// Every new binding holds a node against adoption's node locks, then reads
// the committed adoption and checks the authoritative mission tenant.
func checkViewBinding(ctx context.Context, tx pgx.Tx, node, mission string) error {
	var one int
	if err := tx.QueryRow(ctx, `SELECT 1 FROM agentrt.plan_node WHERE plan_node_id=$1 FOR SHARE`, node).Scan(&one); err != nil {
		return err
	}
	root, err := rootOf(ctx, tx, node)
	if err != nil {
		return err
	}
	var adopted bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agentrt.event WHERE event_type='note' AND ((event_stream_id=$1 AND body_json ? 'plan_view') OR body_json->'external_work'->>'root'=$2))`, PlanStream(root), root).Scan(&adopted); err != nil {
		return err
	}
	if !adopted {
		return nil
	}
	var tenant string
	if err = tx.QueryRow(ctx, `SELECT tenant FROM agentrt.mission WHERE mission_id=$1 FOR SHARE`, mission).Scan(&tenant); err != nil {
		return err
	}
	if tenant != "dev" {
		return invalid("a dev plan cannot bind a %s mission", tenant)
	}
	return nil
}

func adoptPlanView(ctx context.Context, tx pgx.Tx, actor, root string, decision int64, c PlanChange) error {
	doc, err := viewArtifact(ctx, tx, c.Artifact)
	if err != nil {
		return err
	}
	views, err := adoptedViews(ctx, tx)
	if err != nil {
		return err
	}
	previous := views[root]
	if c.ExpectedVersion == nil || *c.ExpectedVersion != previous.Version || doc.Version != previous.Version+1 {
		return fmt.Errorf("%w: plan view version changed", ErrConflict)
	}
	if doc.Root != root {
		return invalid("artifact root mismatch")
	}
	// Lock every existing node, including unclassified steps. A concurrent task
	// proposal already holds its step FOR SHARE and must finish before this scan.
	rows, err := tx.Query(ctx, viewTree+`SELECT n.plan_node_id,COALESCE(n.parent_plan_node_id,''),n.label FROM tree JOIN agentrt.plan_node n USING(plan_node_id) ORDER BY n.plan_node_id FOR UPDATE OF n`, root)
	if err != nil {
		return err
	}
	type node struct{ id, parent, label string }
	nodes, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (node, error) {
		var n node
		err := r.Scan(&n.id, &n.parent, &n.label)
		return n, err
	})
	if err != nil {
		return err
	}
	if len(nodes) == 0 {
		return pgx.ErrNoRows
	}
	if len(nodes) > planview.MaxNodes {
		return invalid("plan exceeds 500 nodes")
	}
	mapping := map[string]string{}
	mappedIDs := map[string]bool{}
	parents := map[string]string{}
	for _, n := range nodes {
		parents[n.id] = n.parent
	}
	for _, item := range doc.Items {
		for _, n := range nodes {
			if n.id == item.Node || n.label == item.Node {
				if mapping[item.Node] != "" && mapping[item.Node] != n.id {
					return invalid("ambiguous node %s", item.Node)
				}
				mapping[item.Node] = n.id
			}
		}
		if mapping[item.Node] == "" {
			return invalid("missing node %s", item.Node)
		}
		if mappedIDs[mapping[item.Node]] {
			return invalid("multiple logical nodes map to %s", mapping[item.Node])
		}
		mappedIDs[mapping[item.Node]] = true
	}
	for _, item := range doc.Items {
		parent := root
		if item.Parent != root {
			parent = mapping[item.Parent]
		}
		if parents[mapping[item.Node]] != parent {
			return invalid("parent mismatch for %s", item.Node)
		}
	}
	if err = devTree(ctx, tx, root); err != nil {
		return err
	}
	docs := map[string]*planview.Document{root: doc}
	for r, a := range views {
		if r != root {
			docs[r], err = viewArtifact(ctx, tx, a.Artifact)
			if err != nil {
				return err
			}
			if err = devTree(ctx, tx, r); err != nil {
				return err
			}
		}
	}
	if _, err = planview.Resolve(root, docs); err != nil {
		return invalid("plan graph: %v", err)
	}
	if err = installPlanBindings(ctx, tx, doc, mapping, actor, decision); err != nil {
		return err
	}
	a := viewAdoption{1, "agentrt.plan_view/v1", root, "dev", doc.Version, c.Artifact, mapping, decision}
	body, err := jsonObject(a)
	if err != nil {
		return err
	}
	_, err = Append(ctx, tx, Event{Stream: PlanStream(root), Type: "note", Actor: actor, Cause: decision, Body: map[string]any{"text": "Plan view adopted", "plan_view": body}})
	return err
}

func jsonObject(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	out, err := canon.Parse(b)
	if err != nil {
		return nil, err
	}
	return out.(map[string]any), nil
}

// Subprotocols are closed even when an internal caller uses Append directly.
func checkViewNote(body map[string]any) error {
	if raw, ok := body["plan_observation"]; ok {
		var n planObservation
		if err := strictViewValue(raw, &n); err != nil {
			return err
		}
		return n.validate()
	}
	if raw, ok := body["plan_view"]; ok {
		var a viewAdoption
		if err := strictViewValue(raw, &a); err != nil {
			return err
		}
		if a.V != 1 || a.Kind != "agentrt.plan_view/v1" || a.Root == "" || a.Tenant != "dev" || a.Version < 1 || a.Artifact == "" || len(a.Nodes) == 0 {
			return invalid("invalid plan_view note")
		}
	}
	if raw, ok := body["external_work"]; ok {
		var n externalNote
		if err := strictViewValue(raw, &n); err != nil {
			return err
		}
		return n.validate()
	}
	return nil
}

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// Known subprotocols are validated at artifact ingress as well as adoption;
// an arbitrary other artifact remains compatible with the existing API.
func validateViewArtifact(kind string, raw []byte) error {
	if kind != "other" {
		return nil
	}
	var head struct {
		Kind string `json:"kind"`
	}
	if json.Unmarshal(raw, &head) != nil {
		return nil
	}
	switch head.Kind {
	case planview.Kind:
		if _, err := planview.Parse(raw); err != nil {
			return invalid("plan view artifact: %v", err)
		}
	case "agentrt.external_work/v1":
		if _, err := parseExternalDescription(raw); err != nil {
			return err
		}
	}
	return nil
}
