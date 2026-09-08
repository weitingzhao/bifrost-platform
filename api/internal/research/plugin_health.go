package research

import (
	"context"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// PluginHealth answers /metrics. Research reports only whether it is reachable,
// so reachability is derived rather than read.
func (h *Handler) PluginHealth(ctx context.Context) probe.PluginHealth {
	st := h.svc.Status(ctx)
	reach := probe.ReachFail
	if st.Reachable {
		reach = probe.ReachOK
	}
	return probe.PluginHealth{
		Name:         "research",
		Reachable:    st.Reachable,
		Reachability: reach,
	}
}
