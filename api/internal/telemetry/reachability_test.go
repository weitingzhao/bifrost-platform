package telemetry

import (
	"strings"
	"testing"
)

func TestReachabilityKeepsOrderAndUnknownState(t *testing.T) {
	var b strings.Builder
	WriteReachability(&b, []ReachabilityObservation{{Name: "second", Reachable: true, State: "ready"}, {Name: "first", State: "unlisted"}}, []string{"ready", "unknown"})
	want := "# HELP bifrost_plugin_reachable Whether platform-api could reach the plugin (1) or not (0)\n# TYPE bifrost_plugin_reachable gauge\nbifrost_plugin_reachable{plugin=\"second\"} 1\nbifrost_plugin_reachable{plugin=\"first\"} 0\n# HELP bifrost_plugin_reachability Plugin reachability, one series per state, 1 on the current one\n# TYPE bifrost_plugin_reachability gauge\nbifrost_plugin_reachability{plugin=\"second\",state=\"ready\"} 1\nbifrost_plugin_reachability{plugin=\"second\",state=\"unknown\"} 0\nbifrost_plugin_reachability{plugin=\"first\",state=\"ready\"} 0\nbifrost_plugin_reachability{plugin=\"first\",state=\"unknown\"} 0\n"
	if b.String() != want {
		t.Fatalf("%s", b.String())
	}
	b.Reset()
	WriteReachability(&b, nil, nil)
	if strings.Contains(b.String(), "{plugin=") || strings.Count(b.String(), "# TYPE") != 2 {
		t.Fatal(b.String())
	}
}
