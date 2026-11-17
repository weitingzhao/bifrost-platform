package planstore

import "github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"

// Keep the database adoption adapter here; progress projection belongs to the plan domain.
func viewExecutionSummary(nodes []map[string]any, graph planview.Graph, adoption viewAdoption) (map[string]any, []any, []any) {
	return planview.ExecutionSummary(nodes, graph, adoption.Nodes)
}
