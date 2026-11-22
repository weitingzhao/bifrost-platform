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

// Origin: 2026-11-18, the Owner's "first make problems visible": a drill must
// reach the stream without passing for real traffic of the topic it borrows.
func TestADrillIsCountedUnderItsOwnStream(t *testing.T) {
	if got := countedAs(Row{Type: "alert.rule.fired", Stream: "drill:N06"}); got != "drill:N06" {
		t.Fatalf("a drill counted as %q", got)
	}
	if got := countedAs(Row{Type: "alert.rule.fired", Stream: "alert:NodeDown"}); got != "alert.rule.fired" {
		t.Fatalf("a real alert counted as %q", got)
	}
}

// Origin: 2026-11-19, the Owner: data that grows keeps a fixed size; the two
// streams had limits but no reading of how full they are.
func TestStreamSizesAreWritten(t *testing.T) {
	recordStreamSize("EV_ROCKET_TEST", 2048, 7)
	var b strings.Builder
	writeStreamSizes(&b)
	for _, want := range []string{`bifrost_bus_stream_bytes{stream="EV_ROCKET_TEST"} 2048`, `bifrost_bus_stream_messages{stream="EV_ROCKET_TEST"} 7`} {
		if !strings.Contains(b.String(), want) {
			t.Fatalf("no %q in %s", want, b.String())
		}
	}
}

// Origin: 2026-11-21, the Bus page listed "backlog per subscription" as not
// measured. Each durable consumer's backlog is written: what the stream holds
// for it not handed yet, and what it was handed and has not acknowledged.
func TestConsumerBacklogsAreWritten(t *testing.T) {
	recordConsumerBacklogs(map[string]uint64{"alert-letters": 3, "headrelease": 0}, map[string]uint64{"alert-letters": 1, "headrelease": 0})
	var b strings.Builder
	writeConsumerBacklogs(&b)
	for _, want := range []string{`bifrost_bus_consumer_pending{consumer="alert-letters"} 3`, `bifrost_bus_consumer_ack_pending{consumer="alert-letters"} 1`, `bifrost_bus_consumer_pending{consumer="headrelease"} 0`} {
		if !strings.Contains(b.String(), want) {
			t.Fatalf("no %q in %s", want, b.String())
		}
	}
}
