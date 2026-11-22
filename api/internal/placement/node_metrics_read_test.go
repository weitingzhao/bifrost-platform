package placement

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/telemetry"
	"k8s.io/apimachinery/pkg/runtime"
)

// 2026-11-22: 02's root disk was about 53% used and growing about 10.8 GB/day;
// the compute line set 70% as the stop line; CI agreed on 11-22 to add the
// delivery port field only. Positive controls: 69%, an
// unprotected node and absent readings still accept work. Exercise the reader
// through Place so both spread (including the current node) and fallback obey it.
func TestRootDiskUsageStopsProtectedPlacement(t *testing.T) {
	for _, tc := range []struct {
		name      string
		protected bool
		root      float64
		missing   bool
		readError bool
		blocked   bool
	}{
		{name: "at 70 percent", protected: true, root: 0.70, blocked: true},
		{name: "above 70 percent", protected: true, root: 0.90, blocked: true},
		{name: "69 percent", protected: true, root: 0.69},
		{name: "unprotected high usage", root: 0.95},
		{name: "zero root reading", protected: true},
		{name: "missing root series", protected: true, missing: true},
		{name: "root query failed", protected: true, readError: true},
		{name: "root is NaN", protected: true, root: math.NaN()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node := placementNode("candidate", true, false, "16")
			if tc.protected {
				node.Labels[batchLabel] = "deny"
			}
			reader := &PromNodeMetrics{}
			metrics := func(ctx context.Context) (map[string]NodeMetrics, error) {
				return reader.Read(ctx, func(_ context.Context, q string) ([]telemetry.SamplePoint, error) {
					value := 0.1
					switch {
					case q == nodeRootDiskUsedQuery:
						if tc.readError {
							return nil, errors.New("root filesystem query unavailable")
						}
						if tc.missing {
							return nil, nil
						}
						value = tc.root
					case q == nodeLatencyQuery || q == nodeRecentLatencyQuery:
						value = 0.001
					case strings.Contains(q, "node_memory_MemAvailable_bytes"):
						value = 0.6
					}
					return []telemetry.SamplePoint{{Labels: map[string]string{"instance": "ip-candidate:9100"}, Value: value}}, nil
				})
			}
			readings, err := metrics(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			wantRoot := tc.root
			if tc.missing || tc.readError || math.IsNaN(tc.root) {
				wantRoot = 0
			}
			if got, ok := readings["ip-candidate"]; !ok || got.RootDiskUsed != wantRoot {
				t.Fatalf("root reading = %+v, present=%v; want %v", got, ok, wantRoot)
			}
			pl := fakePlacer([]runtime.Object{node}, metrics)
			got, placed := pl.Place(context.Background(), unitTask, "candidate")
			want := "candidate"
			if tc.blocked {
				want = ""
			}
			if !placed || got.Node != want {
				t.Fatalf("placement = %+v, enabled=%v; want node %q", got, placed, want)
			}
			if tc.blocked && !strings.Contains(got.Why, "root disk") {
				t.Fatalf("missing root disk refusal: %+v", got)
			}
		})
	}
}
