package planreadings

import "github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"

// ProjectTree formats ordered roots; architecture evidence remains the caller's callback.
func ProjectTree(roots []*TreeRoot, architecture func([]*TreeItem) (any, error)) ([]any, error) {
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
				"count": treeCountDocument(plan.CountSteps(it.Steps)), "active_steps": nonNilActive(it.Active)})
		}
		doc["count"], doc["items"] = treeCountDocument(plan.CountSteps(all)), items
		out = append(out, doc)
	}
	return out, nil
}
func treeCountDocument(c plan.Count) map[string]any {
	return map[string]any{"done": c.Done, "total": c.Total, "manual": c.Manual, "stopped": c.Stopped, "state": c.State}
}
func nonNilActive(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
