package flexquery

import (
	"context"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// PluginHealth answers /metrics with the reachability every plugin reports.
func (h *Handler) PluginHealth(ctx context.Context) probe.PluginHealth {
	st := h.svc.Status(ctx)
	return probe.PluginHealth{
		Name:         "flex-query",
		Reachable:    st.Reachable,
		Reachability: st.Reachability,
	}
}
