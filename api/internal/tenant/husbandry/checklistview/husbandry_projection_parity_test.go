package checklistview

import (
	"reflect"
	"strings"
	"testing"

	husbandry "github.com/weitingzhao/bifrost-platform/api/internal/tenant/husbandry"
)

func TestHusbandryProjectionMatchesOriginal(t *testing.T) {
	verdicts := []string{"healthy", "due", "draining", "missed", "degraded", "caution", "", "UNKNOWN", " HeAlThY "}
	sources := []string{"", "db", "none", " NONE "}
	details := []string{"", "source=none", "TOKEN SOURCE=NONE", strings.Repeat("界", 100)}
	for _, v := range verdicts {
		for _, src := range sources {
			for _, detail := range details {
				for mask := 0; mask < 8; mask++ {
					snap := husbandry.Snapshot{}
					for i, id := range []string{"market_batch", "flex_batch", "research_olap"} {
						if mask&(1<<i) != 0 {
							snap.Lanes = append(snap.Lanes, husbandry.LaneView{ID: id, Verdict: v, Source: src, Detail: detail})
						}
					}
					want, got := baselineHusbandrySignals(snap), Signals(snap)
					if !reflect.DeepEqual(want, got) {
						t.Fatalf("v=%q src=%q mask=%d: %#v != %#v", v, src, mask, want, got)
					}
				}
			}
		}
	}
	snap := husbandry.Snapshot{Lanes: []husbandry.LaneView{{ID: "market_batch", Verdict: "missed"}, {ID: "market_batch", Verdict: "healthy"}, {ID: "ignored", Verdict: "missed"}}}
	if !reflect.DeepEqual(baselineHusbandrySignals(snap), Signals(snap)) {
		t.Fatal("duplicate/unknown lane changed")
	}
}
