package gatecluster

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
)

func nodeObj(name, ready string, cordoned bool, heartbeat string) *unstructured.Unstructured {
	spec := map[string]any{}
	if cordoned {
		spec["unschedulable"] = true
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": name},
		"spec":     spec,
		"status": map[string]any{"conditions": []any{
			map[string]any{"type": "MemoryPressure", "status": "False"},
			map[string]any{"type": "Ready", "status": ready, "lastHeartbeatTime": heartbeat},
		}},
	}}
}

func nodesRow(t *testing.T) WatchSpec {
	t.Helper()
	rows, err := registry.Watches()
	if err != nil {
		t.Fatal(err)
	}
	tenants, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range rows["rocket"] {
		if w.ID == "nodes" {
			return WatchSpec{Watch: w, Ref: tenants["rocket"].Channels[w.Type].Ref}
		}
	}
	t.Fatal("no nodes row in watches/rocket.yaml")
	return WatchSpec{}
}

// Origin: 2026-11-20 02:50Z, ubt-k3s-05 was cordoned and drained for a disk
// swap; NATS, whose disk was on it, sat Pending for an hour and the bus said
// nothing of the node. The node watch is now a row of configuration
// (the Owner: a module that joins must not mean a code change); each change
// of Ready or cordon is said once, the heartbeat says nothing.
func TestAWatchedObjectIsSaidOncePerChange(t *testing.T) {
	w := nodesRow(t)
	l := &memLedger{}
	g := &Gate{Ledger: l, Logf: func(string, ...any) {}}
	g.init()
	ctx := context.Background()
	g.observeWatched(ctx, w, nodeObj("ubt-k3s-05", "True", false, "t1"))
	g.observeWatched(ctx, w, nodeObj("ubt-k3s-05", "True", false, "t2")) // heartbeat only
	g.observeWatched(ctx, w, nodeObj("ubt-k3s-05", "True", true, "t3"))  // cordoned
	g.observeWatched(ctx, w, nodeObj("ubt-k3s-05", "Unknown", true, "t4"))
	got := l.events("node:ubt-k3s-05")
	if len(got) != 3 {
		t.Fatalf("%d events: %+v", len(got), got)
	}
	g2 := &Gate{Ledger: l, Logf: func(string, ...any) {}}
	g2.init()
	g2.observeWatched(ctx, w, nodeObj("ubt-k3s-05", "Unknown", true, "t5"))
	if n := len(l.events("node:ubt-k3s-05")); n != 3 {
		t.Fatalf("a restart repeated the state: %d events", n)
	}
}

func TestAWatchReadsStateByItsRowAndTheRegisterChecksTheBody(t *testing.T) {
	w := nodesRow(t)
	cases := []struct {
		obj        *unstructured.Unstructured
		ready, sch string
	}{
		{nodeObj("ubt-k3s-01", "True", false, ""), "ready", "schedulable"},
		{nodeObj("gpu-server", "Unknown", true, ""), "unknown", "cordoned"},
		{nodeObj("ubt-k3s-02", "False", false, ""), "notready", "schedulable"},
		// No Ready condition at all (a node just joining): the map's default.
		{&unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"name": "ubt-k3s-08"}}}, "unknown", "schedulable"},
	}
	for _, c := range cases {
		subject, values := stateOf(w.Watch, c.obj.Object)
		if subject != c.obj.GetName() || values[0] != c.ready || values[1] != c.sch {
			t.Fatalf("%s: got %s %v, want %s/%s", c.obj.GetName(), subject, values, c.ready, c.sch)
		}
		if err := eventlog.CheckBody(w.Type, watchBody(w, subject, values)); err != nil {
			t.Fatal(err)
		}
	}
	// Positive control: a body whose change does not follow from its fields is refused by the register's rules.
	bad := watchBody(w, "ubt-k3s-01", []string{"ready", "schedulable"})
	bad["change"] = "notready-cordoned"
	if err := eventlog.CheckBody(w.Type, bad); err == nil {
		t.Fatal("a body with a made-up change passed")
	}
	bad = watchBody(w, "ubt-k3s-01", []string{"maybe", "schedulable"})
	if err := eventlog.CheckBody(w.Type, bad); err == nil {
		t.Fatal("a state value outside its pattern passed")
	}
}

func TestFieldPathsReadListsByKeyAndScalars(t *testing.T) {
	obj := map[string]any{
		"metadata": map[string]any{"name": "nats", "namespace": "data"},
		"status":   map[string]any{"readyReplicas": int64(1), "conditions": []any{map[string]any{"type": "Available", "status": "True"}}},
		"spec":     map[string]any{"paused": false},
	}
	for path, want := range map[string]string{
		"metadata.name": "nats", "status.readyReplicas": "1", "spec.paused": "false",
		"status.conditions[type=Available].status": "True",
	} {
		if got, ok := fieldAt(obj, path); !ok || got != want {
			t.Errorf("%s: got %q %v, want %q", path, got, ok, want)
		}
	}
	for _, path := range []string{"status.missing", "status.conditions[type=Gone].status", "metadata.name.deeper"} {
		if got, ok := fieldAt(obj, path); ok {
			t.Errorf("%s: got %q, want missing", path, got)
		}
	}
}
