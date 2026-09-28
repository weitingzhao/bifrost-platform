package ibgateway

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// PluginHealth answers /metrics. Beyond the reachability every plugin reports,
// the gateway contributes the two numbers that would have caught the
// 2026-09-07 outage: whether each broker slot is connected, and how long since
// the ingest last saw a message. The pod stayed 1/1 Running throughout, so
// nothing derived from Kubernetes could have noticed.
func (h *Handler) PluginHealth(ctx context.Context) probe.PluginHealth {
	st := h.svc.Status(ctx)
	out := probe.PluginHealth{
		Name:         "ib-gateway",
		Reachable:    st.Reachable,
		Reachability: st.Reachability,
	}

	// The account agent publishes one flag per slot; absent means not connected.
	for slot, field := range map[string]string{"host": "host_connected", "secondary": "secondary_connected"} {
		out.Gauges = append(out.Gauges, probe.Gauge{
			Key:    "ib_gateway_slot_connected",
			Labels: map[string]string{"slot": slot},
			Value:  boolGauge(st.AccountHealth[field]),
			Help:   "1 when the IB gateway holds a live TWS session for this slot",
		})
	}

	if age, ok := ageSeconds(st.IngestorHealth["last_msg_ts"]); ok {
		out.Gauges = append(out.Gauges, probe.Gauge{
			Key:   "ib_gateway_ingest_last_msg_age_seconds",
			Value: age,
			Help:  "Seconds since the IB market ingest last received a message",
		})
	}
	if g, ok := optCacheProgressGauge(st.OperatorHealth); ok {
		out.Gauges = append(out.Gauges, g)
	}
	return out
}

// optCacheProgressGauge exposes the gateway's option-quote cache heartbeat. On
// 2026-09-28 that loop had silently stopped in a 20-day-old pod while the slots
// stayed connected. The operator health hash has no TTL, so the gateway
// publishes the heartbeat as a timestamp and the age is taken here: it keeps
// growing whether the cache loop or the gateway's own health loop stopped.
// Absent before plugin 0.2.5.
func optCacheProgressGauge(operatorHealth map[string]string) (probe.Gauge, bool) {
	age, ok := ageSeconds(operatorHealth["opt_cache_progress_ts"])
	if !ok {
		return probe.Gauge{}, false
	}
	return probe.Gauge{
		Key:   "ib_gateway_opt_cache_progress_age_seconds",
		Value: age,
		Help:  "Seconds since the IB gateway option quote cache loop last made progress",
	}, true
}

func boolGauge(v string) float64 {
	if strings.EqualFold(strings.TrimSpace(v), "true") || strings.TrimSpace(v) == "1" {
		return 1
	}
	return 0
}

// ageSeconds reads a unix timestamp the gateway wrote and returns its age.
// A clock skew that makes it look future-dated reports 0 rather than negative.
func ageSeconds(ts string) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(ts), 64)
	if err != nil || f <= 0 {
		return 0, false
	}
	age := time.Since(time.Unix(int64(f), 0)).Seconds()
	if age < 0 {
		age = 0
	}
	return age, true
}
