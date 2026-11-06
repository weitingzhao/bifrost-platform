package agentrtapi_test

import (
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// GET /leak-value answers the projection for 1 to 12 full weeks (default 8)
// and the current one, and refuses any other number of weeks.
func TestLeakValueRoute(t *testing.T) {
	pool, _ := world(t)
	now := time.Date(2026, 11, 6, 15, 0, 0, 0, time.UTC)
	s := server(t, pool, func(a *agentrtapi.API) { a.Now = func() time.Time { return now } })
	code, out := call(t, s, "viewer", "GET", "leak-value", nil)
	rows, isList := out["rows"].([]any)
	if code != 200 || out["weeks"] != float64(store.LeakValueWeeksDefault) || out["as_of"] != "2026-11-06T15:00:00Z" || !isList || len(rows) != 0 {
		t.Fatalf("default %d %v", code, out)
	}
	if code, out = call(t, s, "viewer", "GET", "leak-value?weeks=12", nil); code != 200 || out["weeks"] != float64(12) {
		t.Fatalf("12 weeks %d %v", code, out)
	}
	for _, bad := range []string{"0", "13", "-1", "eight", "2.5"} {
		if code, out = call(t, s, "viewer", "GET", "leak-value?weeks="+bad, nil); code != 400 {
			t.Fatalf("weeks=%s: %d %v", bad, code, out)
		}
	}
}
