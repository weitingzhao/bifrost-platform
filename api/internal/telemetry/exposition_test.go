package telemetry

import (
	"reflect"
	"strings"
	"testing"
)

func TestGaugeExpositionPreservesOrderingEscapingAndFirstHelp(t *testing.T) {
	labels := map[string]string{"z": "line\nquote\"", "a": "value"}
	gauges := []MetricGauge{{Plugin: "two", Key: "g", Help: "first", Labels: labels, Value: 2}, {Plugin: "one", Key: "g", Help: "ignored", Value: 1}, {Plugin: "one", Key: "a", Value: 0}}
	var b strings.Builder
	WriteGauges(&b, gauges)
	want := "# TYPE bifrost_a gauge\nbifrost_a{plugin=\"one\"} 0\n# HELP bifrost_g first\n# TYPE bifrost_g gauge\nbifrost_g{plugin=\"one\"} 1\nbifrost_g{plugin=\"two\",a=\"value\",z=\"line\\nquote\\\"\"} 2\n"
	if b.String() != want {
		t.Fatalf("got %q want %q", b.String(), want)
	}
	if !reflect.DeepEqual(labels, map[string]string{"z": "line\nquote\"", "a": "value"}) || gauges[0].Plugin != "two" {
		t.Fatal("input mutated")
	}
	b.Reset()
	WriteGauges(&b, nil)
	if b.Len() != 0 {
		t.Fatal("empty emitted gauges")
	}
}
