package ibgateway

import (
	"strconv"
	"testing"
	"time"
)

func TestOptCacheProgressGaugeAgesTheHeartbeat(t *testing.T) {
	ts := strconv.FormatFloat(float64(time.Now().Add(-90*time.Second).Unix()), 'f', 3, 64)
	g, ok := optCacheProgressGauge(map[string]string{"opt_cache_progress_ts": ts})
	if !ok {
		t.Fatal("gauge missing for a valid heartbeat")
	}
	if g.Key != "ib_gateway_opt_cache_progress_age_seconds" {
		t.Fatalf("key = %q", g.Key)
	}
	if g.Value < 89 || g.Value > 95 {
		t.Fatalf("age = %v, want ~90", g.Value)
	}
}

func TestOptCacheProgressGaugeAbsentWithoutHeartbeat(t *testing.T) {
	// Plugin <0.2.5 writes no timestamp; the gauge must be absent, not 0.
	for _, h := range []map[string]string{nil, {}, {"opt_cache_progress_ts": ""}, {"opt_cache_progress_ts": "x"}} {
		if _, ok := optCacheProgressGauge(h); ok {
			t.Fatalf("gauge present for %v", h)
		}
	}
}
