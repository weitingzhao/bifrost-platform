package assembly

import (
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/fullci"
	"github.com/weitingzhao/bifrost-platform/api/internal/cirunview"
)

// WriteMetrics writes the verification's series on /metrics: whether the
// newest finished daily full CI run is red, for the alert rules (only the
// process whose controller runs that loop, workers on PROD, has the answer:
// full is nil, or has no finished run, elsewhere and writes no series), and
// the controller's counts of runs recorded by event and missed by events.
// Moved out of server (metrics.go, writeFullCIMetric and the controller's
// line of handleMetrics) on 2026-11-23.
func WriteMetrics(b *strings.Builder, full *fullci.FullCI) {
	if name, status, ok := full.Latest(); ok {
		cirunview.WriteFullCIMetric(b, name, status)
	}
	controller.WritePipelineEventMetrics(b)
}
