package server

import (
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

func TestWritePluginGaugesGroupsHelpOncePerMetric(t *testing.T) {
	var b strings.Builder
	writePluginGauges(&b, []probe.PluginHealth{
		{Name: "ib-gateway", Gauges: []probe.Gauge{
			{Key: "ib_gateway_slot_connected", Labels: map[string]string{"slot": "secondary"}, Value: 0, Help: "h"},
			{Key: "ib_gateway_slot_connected", Labels: map[string]string{"slot": "host"}, Value: 1, Help: "h"},
			{Key: "ib_gateway_ingest_last_msg_age_seconds", Value: 12, Help: "age"},
		}},
		{Name: "market-data"},
	})
	out := b.String()

	// The exposition format allows one HELP and one TYPE per metric name.
	if n := strings.Count(out, "# HELP bifrost_ib_gateway_slot_connected"); n != 1 {
		t.Fatalf("HELP repeated %d times:\n%s", n, out)
	}
	if n := strings.Count(out, "# TYPE bifrost_ib_gateway_slot_connected"); n != 1 {
		t.Fatalf("TYPE repeated %d times:\n%s", n, out)
	}
	for _, want := range []string{
		`bifrost_ib_gateway_slot_connected{plugin="ib-gateway",slot="host"} 1`,
		`bifrost_ib_gateway_slot_connected{plugin="ib-gateway",slot="secondary"} 0`,
		`bifrost_ib_gateway_ingest_last_msg_age_seconds{plugin="ib-gateway"} 12`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	// A plugin contributing no gauges must not emit an empty metric block.
	if strings.Contains(out, `plugin="market-data"`) {
		t.Fatalf("market-data has no gauges but appears:\n%s", out)
	}
}

func TestGaugeOutputIsStableAcrossScrapes(t *testing.T) {
	// Map iteration order must not leak into the exposition, or every scrape
	// looks like a change and diffing two scrapes becomes useless.
	h := []probe.PluginHealth{{Name: "ib-gateway", Gauges: []probe.Gauge{
		{Key: "a_metric", Labels: map[string]string{"z": "1", "a": "2"}, Value: 1},
		{Key: "b_metric", Value: 2},
	}}}
	var first strings.Builder
	writePluginGauges(&first, h)
	for i := 0; i < 20; i++ {
		var again strings.Builder
		writePluginGauges(&again, h)
		if again.String() != first.String() {
			t.Fatalf("unstable output:\n%s\n---\n%s", first.String(), again.String())
		}
	}
	if !strings.Contains(first.String(), `{plugin="ib-gateway",a="2",z="1"}`) {
		t.Fatalf("labels not sorted:\n%s", first.String())
	}
}
