package relay

import (
	"strings"
	"testing"
	"time"
)

// 2026-11-17: whether the first release.deploy.finished went out could not be
// told without a token (the published total names no type). The relay now
// writes, per event type, how many it published and when the last one was.
func TestRelayMetricsNameEachType(t *testing.T) {
	at := time.Unix(1794940060, 0)
	publishedType("release.deploy.finished", at)
	publishedType("release.deploy.finished", at)
	publishedType("ci.run.finished", at.Add(-time.Minute))
	var b strings.Builder
	WriteMetrics(&b)
	out := b.String()
	for _, want := range []string{
		`bifrost_bus_relay_published_by_type_total{type="release.deploy.finished"} 2`,
		`bifrost_bus_relay_published_by_type_total{type="ci.run.finished"} 1`,
		`bifrost_bus_relay_last_published_timestamp_seconds{type="release.deploy.finished"} 1794940060`,
		`bifrost_bus_relay_published_total `,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
