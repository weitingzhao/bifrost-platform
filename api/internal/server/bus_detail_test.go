//bifrost:owner coordination
package server

import (
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/regressions"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

// 2026-11-17: the release context lists the open regressions of its
// repository (5d0ac234 reached PROD carrying a known one); each keeps which
// commit, what is broken, who said so and when, and none is an empty list.
func TestKnownRegressionsCarryWhatTheReleaserReads(t *testing.T) {
	at := time.Date(2026, 11, 17, 21, 29, 0, 0, time.FixedZone("CST", -6*3600))
	got := knownRegressions([]regressions.Regression{{Repo: "bifrost-platform", Introduced: "a6862d84", Impact: "module activity", By: "thread:local_x", KnownAt: at}})
	want := delivery.KnownRegression{Introduced: "a6862d84", Impact: "module activity", By: "thread:local_x", KnownAt: "2026-11-18T03:29:00Z"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %+v", got)
	}
	if none := knownRegressions(nil); none == nil || len(none) != 0 {
		t.Fatalf("none open = %#v", none)
	}
}
