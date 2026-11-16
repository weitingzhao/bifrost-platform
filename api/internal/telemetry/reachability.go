package telemetry

import (
	"fmt"
	"strings"
)

// ReachabilityObservation is already evaluated by the probe; this formatter makes no health decision.
type ReachabilityObservation struct {
	Name      string
	Reachable bool
	State     string
}

// WriteReachability preserves observation and state order, including all zero series for an unknown state.
func WriteReachability(b *strings.Builder, observations []ReachabilityObservation, states []string) {
	b.WriteString("# HELP bifrost_plugin_reachable Whether platform-api could reach the plugin (1) or not (0)\n")
	b.WriteString("# TYPE bifrost_plugin_reachable gauge\n")
	for _, h := range observations {
		fmt.Fprintf(b, "bifrost_plugin_reachable{plugin=%q} %d\n", h.Name, reachabilityBit(h.Reachable))
	}

	// One series per state so an alert can say `state="ok"` without string
	// comparison, and a plugin flipping state leaves no stale series behind.
	b.WriteString("# HELP bifrost_plugin_reachability Plugin reachability, one series per state, 1 on the current one\n")
	b.WriteString("# TYPE bifrost_plugin_reachability gauge\n")
	for _, h := range observations {
		for _, st := range states {
			fmt.Fprintf(b, "bifrost_plugin_reachability{plugin=%q,state=%q} %d\n",
				h.Name, st, reachabilityBit(h.State == st))
		}
	}

}
func reachabilityBit(v bool) int {
	if v {
		return 1
	}
	return 0
}
