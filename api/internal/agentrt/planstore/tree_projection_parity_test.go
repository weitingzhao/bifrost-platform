package planstore

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planreadings"
)

func frozenTree(roots []*planreadings.TreeRoot, architecture func([]*planreadings.TreeItem) (any, error)) ([]any, error) {
	out := make([]any, 0, len(roots))
	for _, r := range roots {
		doc := map[string]any{"plan_node_id": r.ID, "kind": r.Kind, "title": r.Title}
		if r.Kind == "arch_root" {
			var err error
			if doc["rows"], err = architecture(r.Items); err != nil {
				return nil, err
			}
			out = append(out, doc)
			continue
		}
		var all []plan.Step
		items := make([]any, 0, len(r.Items))
		for _, it := range r.Items {
			all = append(all, it.Steps...)
			items = append(items, map[string]any{"plan_node_id": it.ID, "label": it.Label, "title": it.Title,
				"count": countDoc(plan.CountSteps(it.Steps)), "active_steps": nonNil(it.Active)})
		}
		doc["count"], doc["items"] = countDoc(plan.CountSteps(all)), items
		out = append(out, doc)
	}
	return out, nil
}

func TestTreeProjectionFrozenParity(t *testing.T) {
	t.Parallel()
	for _, states := range [][]string{nil, {"planned"}, {"done", "active", "stopped", "unknown"}} {
		for _, fail := range []bool{false, true} {
			roots := []*planreadings.TreeRoot{{ID: "empty", Kind: "root"}, {ID: "arch", Kind: "arch_root", Items: []*planreadings.TreeItem{{ID: "row", Title: "row"}}}, {ID: "round", Kind: "root", Items: []*planreadings.TreeItem{{ID: "item", Title: "<>&", Active: []string{"b", "a"}}}}}
			for i, s := range states {
				roots[2].Items[0].Steps = append(roots[2].Items[0].Steps, plan.Step{ID: fmt.Sprint(i), State: s, Manual: i%2 == 0})
			}
			var oldTrace, newTrace []string
			cb := func(trace *[]string, items []*planreadings.TreeItem) (any, error) {
				for _, i := range items {
					*trace = append(*trace, i.ID)
				}
				if fail {
					return nil, fmt.Errorf("evidence unavailable")
				}
				return []any{map[string]any{"unknown": "unchanged"}}, nil
			}
			want, we := frozenTree(roots, func(i []*planreadings.TreeItem) (any, error) { return cb(&oldTrace, i) })
			got, ge := planreadings.ProjectTree(roots, func(i []*planreadings.TreeItem) (any, error) { return cb(&newTrace, i) })
			if !reflect.DeepEqual(want, got) || fmt.Sprint(we) != fmt.Sprint(ge) || !reflect.DeepEqual(oldTrace, newTrace) {
				t.Fatalf("tree differs old=%#v %v new=%#v %v", want, we, got, ge)
			}
		}
	}
}
