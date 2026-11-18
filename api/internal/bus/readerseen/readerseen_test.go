package readerseen

import (
	"strings"
	"testing"
	"time"
)

// Origin: 2026-11-18, the coordinator: a reader marked moved must show when it
// last really ran on its new source; these are the two numbers the page reads.
func TestAMarkCountsAndKeepsTheLastTime(t *testing.T) {
	at := time.Unix(1794990000, 0)
	sawAt("test-reader", at.Add(-time.Minute))
	sawAt("test-reader", at)
	var b strings.Builder
	WriteMetrics(&b)
	for _, want := range []string{MetricRuns + `{reader="test-reader"} 2`, MetricLast + `{reader="test-reader"} 1794990000`} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("missing %q in:\n%s", want, b.String())
		}
	}
}
