package gatecluster

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

func node(name, ready string, cordoned bool, heartbeat string) *unstructured.Unstructured {
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

// Origin: 2026-11-20 02:50Z, ubt-k3s-05 was cordoned and drained for a disk
// swap; NATS, whose disk was on it, sat Pending for an hour and the bus said
// nothing of the node. Each change of Ready or cordon is said once; the node's
// heartbeat (its object changes every few seconds) says nothing.
func TestANodeIsSaidOncePerChange(t *testing.T) {
	l := &memLedger{}
	g := &Gate{Ledger: l, Logf: func(string, ...any) {}}
	g.init()
	ctx := context.Background()
	g.observeNode(ctx, node("ubt-k3s-05", "True", false, "t1"))
	g.observeNode(ctx, node("ubt-k3s-05", "True", false, "t2")) // heartbeat only
	g.observeNode(ctx, node("ubt-k3s-05", "True", true, "t3"))  // cordoned
	g.observeNode(ctx, node("ubt-k3s-05", "Unknown", true, "t4"))
	got := l.events(NodeStream("ubt-k3s-05"))
	if len(got) != 3 {
		t.Fatalf("%d events: %+v", len(got), got)
	}
	// A new process remembers nothing and asks the ledger: no repeat.
	g2 := &Gate{Ledger: l, Logf: func(string, ...any) {}}
	g2.init()
	g2.observeNode(ctx, node("ubt-k3s-05", "Unknown", true, "t5"))
	if n := len(l.events(NodeStream("ubt-k3s-05"))); n != 3 {
		t.Fatalf("a restart repeated the state: %d events", n)
	}
}

func TestNodeStateIsReadAndItsBodyChecks(t *testing.T) {
	cases := []struct {
		obj        *unstructured.Unstructured
		ready, sch string
	}{
		{node("ubt-k3s-01", "True", false, ""), "ready", "schedulable"},
		{node("gpu-server", "Unknown", true, ""), "unknown", "cordoned"},
		{node("ubt-k3s-02", "False", false, ""), "notready", "schedulable"},
		// No Ready condition at all (a node just joining): unknown.
		{&unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"name": "ubt-k3s-08"}}}, "unknown", "schedulable"},
	}
	for _, c := range cases {
		s, ok := NodeStateOf(c.obj)
		if !ok || s.Ready != c.ready || s.Schedule != c.sch {
			t.Fatalf("%s: got %+v ok=%v, want %s/%s", c.obj.GetName(), s, ok, c.ready, c.sch)
		}
		if err := eventlog.CheckBody(TypeNodeChanged, nodeBody(s)); err != nil {
			t.Fatal(err)
		}
	}
	// Positive control for the check above: a body whose change does not follow from its fields is refused.
	bad := nodeBody(NodeState{Node: "ubt-k3s-01", Ready: "ready", Schedule: "schedulable"})
	bad["change"] = "notready-cordoned"
	if err := eventlog.CheckBody(TypeNodeChanged, bad); err == nil {
		t.Fatal("a body with a made-up change passed")
	}
	if _, ok := NodeStateOf(&unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"name": "Not_A_Node"}}}); ok {
		t.Fatal("a bad node name was taken")
	}
}
