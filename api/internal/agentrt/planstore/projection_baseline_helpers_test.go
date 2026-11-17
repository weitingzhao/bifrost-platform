package planstore

import (
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
)

// Frozen helper implementations used only by pre-extraction output baselines.
func countDoc(c plan.Count) map[string]any {
	return map[string]any{"done": c.Done, "total": c.Total, "manual": c.Manual, "stopped": c.Stopped, "state": c.State}
}

func stamp(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func nonNilAny(s []any) []any {
	if s == nil {
		return []any{}
	}
	return s
}
