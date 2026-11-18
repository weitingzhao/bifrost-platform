package relay

import (
	"strings"
	"testing"
	"time"
)

// Origin: 2026-11-18, the CI line measured the stream's disk at 334 ms average
// write latency and the bus could not say what that cost it. The histogram must
// put a slow publish in a slow bucket and stay cumulative.
func TestThePublishHistogramCountsSlowPublishes(t *testing.T) {
	var before strings.Builder
	writePublishHistogram(&before)
	observePublish(2 * time.Millisecond)
	observePublish(2 * time.Second)
	var after strings.Builder
	writePublishHistogram(&after)
	count := func(text, line string) string {
		for _, l := range strings.Split(text, "\n") {
			if strings.HasPrefix(l, line+" ") {
				return strings.TrimPrefix(l, line+" ")
			}
		}
		t.Fatalf("no %s in %s", line, text)
		return ""
	}
	diff := func(line string) int {
		var a, b int
		for _, c := range count(before.String(), line) {
			a = a*10 + int(c-'0')
		}
		for _, c := range count(after.String(), line) {
			b = b*10 + int(c-'0')
		}
		return b - a
	}
	if got := diff(`bifrost_bus_relay_publish_seconds_bucket{le="0.005"}`); got != 1 {
		t.Errorf("fast bucket grew by %d", got)
	}
	if got := diff(`bifrost_bus_relay_publish_seconds_bucket{le="1"}`); got != 1 {
		t.Errorf("1 s bucket grew by %d, the slow publish is above it", got)
	}
	if got := diff(`bifrost_bus_relay_publish_seconds_bucket{le="5"}`); got != 2 {
		t.Errorf("5 s bucket grew by %d", got)
	}
}
