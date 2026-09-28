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

func gaugeBySlot(slots []SlotStatus) map[string]float64 {
	out := map[string]float64{}
	for _, g := range slotConnectedGauges(slots) {
		out[g.Labels["slot"]] = g.Value
	}
	return out
}

func TestSlotConnectedGaugesFromPerAccountHealth(t *testing.T) {
	got := gaugeBySlot([]SlotStatus{
		{Slot: "host", AccountID: "U0000001", Connected: true},
		{Slot: "secondary", AccountID: "U0000002", Connected: true},
	})
	if got["host"] != 1 || got["secondary"] != 1 {
		t.Fatalf("both connected, got %v", got)
	}
}

func TestSlotConnectedGaugesZeroWhenKeysExpired(t *testing.T) {
	// A hung gateway stops rewriting ib:health:<account>; after the 25s TTL
	// readSlots returns nothing and both slots must read 0, not a frozen 1.
	got := gaugeBySlot(nil)
	if got["host"] != 0 || got["secondary"] != 0 || len(got) != 2 {
		t.Fatalf("no slot keys, got %v", got)
	}
	got = gaugeBySlot([]SlotStatus{{Slot: "host", Connected: true}})
	if got["host"] != 1 || got["secondary"] != 0 {
		t.Fatalf("secondary key gone, got %v", got)
	}
	got = gaugeBySlot([]SlotStatus{{Slot: "host", Status: "reconnecting", Connected: false}})
	if got["host"] != 0 {
		t.Fatalf("host reconnecting, got %v", got)
	}
}
