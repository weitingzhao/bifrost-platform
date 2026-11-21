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

func workloadRow(t *testing.T, id string) WatchSpec {
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
		if w.ID == id {
			return WatchSpec{Watch: w, Ref: tenants["rocket"].Channels[w.Type].Ref}
		}
	}
	t.Fatalf("no %s row", id)
	return WatchSpec{}
}

// 2026-11-20: NATS (a StatefulSet) sat Pending an hour and nothing on the bus
// said so. A workload is ready when its ready replicas equal its replicas;
// one scaled to 0 is ready with none (the API leaves readyReplicas out).
func TestAWorkloadIsReadyWhenItsReplicasAreReady(t *testing.T) {
	w := workloadRow(t, "statefulsets")
	obj := func(replicas int64, ready any) *unstructured.Unstructured {
		status := map[string]any{}
		if ready != nil {
			status["readyReplicas"] = ready
		}
		return &unstructured.Unstructured{Object: map[string]any{
			"metadata": map[string]any{"name": "nats", "namespace": "data"},
			"spec":     map[string]any{"replicas": replicas},
			"status":   status,
		}}
	}
	for _, c := range []struct {
		replicas int64
		ready    any
		want     string
	}{
		{1, int64(1), "ready"},
		{1, nil, "notready"}, // Pending: no ready pod, the field left out
		{3, int64(2), "notready"},
		{0, nil, "ready"}, // scaled to 0 (STG daemon)
	} {
		subject, values := stateOf(w.Watch, obj(c.replicas, c.ready).Object)
		if subject != "data/nats" || values[0] != c.want {
			t.Errorf("replicas %d ready %v: got %s %v, want %s", c.replicas, c.ready, subject, values, c.want)
		}
		if err := eventlog.CheckBody(w.Type, watchBody(w, subject, values)); err != nil {
			t.Fatal(err)
		}
	}
	// Positive control: one Pending after ready writes two events, a repeat none.
	l := &memLedger{}
	g := &Gate{Ledger: l, Logf: func(string, ...any) {}}
	g.init()
	ctx := context.Background()
	g.observeWatched(ctx, w, obj(1, int64(1)))
	g.observeWatched(ctx, w, obj(1, nil))
	g.observeWatched(ctx, w, obj(1, nil))
	if n := len(l.events("sts:data/nats")); n != 2 {
		t.Fatalf("%d events, want 2", n)
	}
}
