package store

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/workregistration"
	"reflect"
	"testing"
	"time"
)

func TestSnapshotDisplayFrozenParity(t *testing.T) {
	t.Parallel()
	order := 2
	doc := &planview.Document{Root: "R", Items: []planview.Item{{Node: "one", Lane: "mainline", Order: &order}, {Node: "two", Lane: "support"}, {Node: "absent", Lane: "mainline", Order: &order}}}
	for _, document := range []*planview.Document{nil, doc} {
		for _, warning := range []string{"", "unavailable"} {
			nodes := func() []map[string]any {
				return []map[string]any{{"plan_node_id": "n1"}, {"plan_node_id": "n2"}, {"plan_node_id": "unbound"}}
			}
			old, new := nodes(), nodes()
			bindings := map[string]string{"one": "n1", "two": "n2"}
			graph := planview.Graph{Mainline: []planview.NodeRef{}, Edges: []planview.Dependency{}}
			items := []planview.Item{}
			warnings := []string{}
			if warning != "" {
				warnings = append(warnings, warning)
			} else if document != nil {
				graph = planview.AdoptedGraph(document)
				items = document.Items
			} else {
				warnings = append(warnings, "Legacy evidence: plan has no adopted view; nodes are Unclassified")
			}
			byID := map[string]map[string]any{}
			for _, n := range old {
				n["lane_kind"] = "Unclassified"
				byID[n["plan_node_id"].(string)] = n
			}
			for _, item := range items {
				if n := byID[bindings[item.Node]]; n != nil {
					n["lane_kind"] = item.Lane
					n["view"] = item
				}
			}
			gotGraph, gotItems, gotWarnings := planview.ProjectSnapshotNodes(new, document, warning, bindings)
			if !reflect.DeepEqual(old, new) || !reflect.DeepEqual(graph, gotGraph) || !reflect.DeepEqual(items, gotItems) || !reflect.DeepEqual(warnings, gotWarnings) {
				t.Fatal("snapshot projection differs")
			}
		}
	}
	now := time.Date(2026, 11, 16, 0, 0, 0, 0, time.FixedZone("offset", 3600))
	observations := func() []map[string]any {
		return []map[string]any{{"external_work_id": "1", "state": "Registered", "ended_at": "2026-11-15T00:00:00Z"}, {"external_work_id": "2", "state": "Running", "ended_at": "2026-11-16T00:00:00Z"}, {"external_work_id": "3", "state": "Done", "ended_at": "short"}, {"external_work_id": "4", "state": "Unconfirmed"}}
	}
	old, new := observations(), observations()
	working, finished := []any{}, []any{}
	var oldTrace, newTrace []string
	verify := func(id string) any { return map[string]any{"id": id, "state": "Unknown"} }
	for _, e := range old {
		oldTrace = append(oldTrace, e["external_work_id"].(string))
		e["verification"] = verify(e["external_work_id"].(string))
		if e["state"] == "Running" || e["state"] == "Registered" {
			working = append(working, e)
		}
		if end, _ := e["ended_at"].(string); len(end) >= 10 && end[:10] == now.UTC().Format("2006-01-02") {
			finished = append(finished, e)
		}
	}
	gw, gf := workregistration.ActivityBuckets(new, now, func(id string) any { newTrace = append(newTrace, id); return verify(id) })
	if !reflect.DeepEqual(old, new) || !reflect.DeepEqual(working, gw) || !reflect.DeepEqual(finished, gf) || !reflect.DeepEqual(oldTrace, newTrace) {
		t.Fatal("activity output or callback order differs")
	}
}
