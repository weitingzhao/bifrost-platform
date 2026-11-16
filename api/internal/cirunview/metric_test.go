package cirunview

import (
	"strings"
	"testing"
)

func TestFullCIMetricWireAndEscaping(t *testing.T) {
	t.Parallel()
	const prefix = "# HELP bifrost_full_ci_red Whether the newest finished daily full CI run is red (1) or green (0)\n# TYPE bifrost_full_ci_red gauge\n"
	for _, tc := range []struct{ status, want string }{{"Failed", "1"}, {"Succeeded", "0"}, {"", "0"}, {"Unknown", "0"}} {
		var b strings.Builder
		WriteFullCIMetric(&b, "ci-\"one\n", tc.status)
		want := prefix + "bifrost_full_ci_red{run=\"ci-\\\"one\\n\"} " + tc.want + "\n"
		if b.String() != want {
			t.Fatalf("%q: %q", tc.status, b.String())
		}
	}
}
